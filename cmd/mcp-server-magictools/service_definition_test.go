package main

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type plistXML struct {
	Dict struct {
		Keys   []string `xml:"key"`
		Values []struct {
			XMLName xml.Name
			Text    string   `xml:",chardata"`
			Items   []string `xml:"string"`
		} `xml:",any"`
	} `xml:"dict"`
}

func plistValues(t *testing.T, text string) map[string]plistXMLValue {
	t.Helper()
	var p plistXML
	if err := xml.Unmarshal([]byte(text), &p); err != nil {
		t.Fatalf("parse plist: %v", err)
	}
	if len(p.Dict.Keys) != len(p.Dict.Values) {
		t.Fatalf("plist has %d keys and %d values", len(p.Dict.Keys), len(p.Dict.Values))
	}
	out := map[string]plistXMLValue{}
	for i, key := range p.Dict.Keys {
		out[key] = plistXMLValue{Type: p.Dict.Values[i].XMLName.Local, Text: strings.TrimSpace(p.Dict.Values[i].Text), Items: p.Dict.Values[i].Items}
	}
	return out
}

type plistXMLValue struct {
	Type, Text string
	Items      []string
}

func testAbs(parts ...string) string {
	if runtime.GOOS == goOSWindows {
		return filepath.Join(`C:\`, filepath.Join(parts...))
	}
	return filepath.Join(string(filepath.Separator), filepath.Join(parts...))
}

func TestLaunchdDefinitionEscapesAndRoundTrips(t *testing.T) {
	bin := testAbs("tmp", "a & b <β>", `"tool"`)
	d := defaultLaunchdDefinition(bin, testAbs("tmp", "home & <β>"), "local.magictools.test.xml")
	d.Environment["MCP_TEST"] = "a & b < c > d \"quoted\""
	text, err := renderLaunchdDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	v := plistValues(t, text)
	if got := v["ProgramArguments"].Items[0]; got != bin {
		t.Fatalf("program = %q, want %q", got, bin)
	}
	if got := v["MCP_TEST"]; got.Text != "" {
		t.Fatalf("environment is nested under a dictionary: %#v", got)
	}
	for key, want := range map[string]string{"ProcessType": "Standard", "Umask": "077", "StandardOutPath": d.LogPath, "StandardErrorPath": d.LogPath} {
		if v[key].Text != want {
			t.Errorf("%s = %q, want %q", key, v[key].Text, want)
		}
	}
	for _, key := range []string{"RunAtLoad", "KeepAlive"} {
		if v[key].Type != "true" {
			t.Errorf("%s is not true", key)
		}
	}
	for key, want := range map[string]string{"ExitTimeOut": "35", "ThrottleInterval": "5"} {
		if v[key].Text != want {
			t.Errorf("%s = %q", key, v[key].Text)
		}
	}
	if runtime.GOOS == goOSDarwin {
		path := filepath.Join(t.TempDir(), "test.plist")
		if err := writeFileSynced(path, []byte(text)); err != nil {
			t.Fatal(err)
		}
		out, err := timedExec("plutil", "-lint", path)
		if err != nil {
			t.Fatalf("plutil -lint: %v: %s", err, out)
		}
	}
}

func TestLaunchdDefinitionRejectsUnsafePaths(t *testing.T) {
	d := defaultLaunchdDefinition("relative/tool", "/tmp", "local.magictools.test")
	if _, err := renderLaunchdDefinition(d); err == nil {
		t.Fatal("relative program accepted")
	}
	d = defaultLaunchdDefinition(testAbs("tmp", "tool"), testAbs("tmp"), "local.magictools.test")
	d.Arguments[0] = testAbs("tmp", "other")
	if _, err := renderLaunchdDefinition(d); err == nil {
		t.Fatal("mismatched program accepted")
	}
}

func TestWriteFileSyncedAtomicAndNoSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.plist")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileSynced(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("contents = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeFileSynced(path, []byte("bad")); err == nil {
		t.Fatal("symlink destination accepted")
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatal("symlink target overwritten")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temporary file left: %s", e.Name())
		}
	}
}

func TestWriteFileSyncedFailuresPreserveOldBytes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plant   func()
		restore func()
	}{
		{"write", func() {
			writeServiceFile = func(*os.File, []byte) (int, error) { return 0, errors.New("planted write") }
		}, func() { writeServiceFile = func(f *os.File, data []byte) (int, error) { return f.Write(data) } }},
		{"rename", func() {
			renameServiceFile = func(*os.Root, string, string) error { return errors.New("planted rename") }
		}, func() {
			renameServiceFile = func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) }
		}},
		{"directory sync", func() { syncServiceDirectoryFile = func(*os.Root) error { return errors.New("planted sync") } }, func() { syncServiceDirectoryFile = syncServiceDirectory }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "job.plist")
			if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			tc.plant()
			t.Cleanup(tc.restore)
			if err := writeFileSynced(path, []byte("new")); err == nil {
				t.Fatal("planted failure accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "old" && tc.name != "directory sync" {
				t.Fatalf("contents = %q", got)
			}
			if tc.name == "directory sync" && string(got) != "new" {
				t.Fatalf("renamed contents = %q", got)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp-") {
					t.Errorf("temporary file left: %s", entry.Name())
				}
			}
		})
	}
}

func TestWriteFileSyncedRejectsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.plist")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileSynced(path, []byte("bad")); err == nil {
		t.Fatal("directory accepted")
	}
	if got, err := os.ReadFile(path); !errors.Is(err, os.ErrInvalid) && len(got) != 0 {
		t.Fatal("directory replaced")
	}
}
