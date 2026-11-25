package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// launchdDefinition is the complete property-list policy installed for one job.
type launchdDefinition struct {
	Label       string
	Program     string
	Arguments   []string
	Environment map[string]string
	WorkingDir  string
	LogPath     string
}

func defaultLaunchdDefinition(binPath, home, label string) launchdDefinition {
	env := map[string]string{
		"HOME": home, "PATH": buildLaunchdPath(home), "MCP_SERVICE_MODE": serviceModeTrue,
		"MCP_ENDPOINT_IDE_PORT": configBindAddr(),
	}
	if v := resolveRecallURL(); v != "" {
		env["MCP_REC_URL"] = v
	}
	if v := resolveSocraticURL(); v != "" {
		env["MCP_SOC_URL"] = v
	}
	return launchdDefinition{
		Label: label, Program: binPath, Arguments: []string{binPath, "serve"},
		Environment: env, WorkingDir: filepath.Join(home, "Library", "Caches", cmdName),
		LogPath: filepath.Join(home, "Library", "Caches", cmdName, "magictools_debug.log"),
	}
}

var configBindAddr = func() string { return config.ResolveBindAddr("") }

func xmlText(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		panic(err)
	}
	return b.String()
}
func xmlKeyString(b *bytes.Buffer, key, value string) {
	fmt.Fprintf(b, "  <key>%s</key>\n  <string>%s</string>\n", xmlText(key), xmlText(value))
}
func xmlBool(b *bytes.Buffer, key string, value bool) {
	v := "false"
	if value {
		v = "true"
	}
	fmt.Fprintf(b, "  <key>%s</key>\n  <%s/>\n", xmlText(key), v)
}
func xmlInt(b *bytes.Buffer, key string, value int) {
	fmt.Fprintf(b, "  <key>%s</key>\n  <integer>%d</integer>\n", xmlText(key), value)
}

// renderLaunchdDefinition escapes every value and emits a deterministic plist.
func renderLaunchdDefinition(d launchdDefinition) (string, error) {
	if d.Label == "" || !filepath.IsAbs(d.Program) || !filepath.IsAbs(d.WorkingDir) || !filepath.IsAbs(d.LogPath) {
		return "", errors.New("launchd label and absolute program, working, and log paths are required")
	}
	if len(d.Arguments) == 0 || d.Arguments[0] != d.Program {
		return "", errors.New("launchd ProgramArguments[0] must equal the absolute program")
	}
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n<dict>\n")
	xmlKeyString(&b, "Label", d.Label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range d.Arguments {
		fmt.Fprintf(&b, "    <string>%s</string>\n", xmlText(arg))
	}
	b.WriteString("  </array>\n  <key>EnvironmentVariables</key>\n  <dict>\n")
	for _, key := range slices.Sorted(maps.Keys(d.Environment)) {
		xmlKeyString(&b, key, d.Environment[key])
	}
	b.WriteString("  </dict>\n")
	b.WriteString("  <key>LimitLoadToSessionType</key>\n  <array>\n    <string>Aqua</string>\n    <string>Background</string>\n    <string>StandardIO</string>\n  </array>\n")
	xmlKeyString(&b, "WorkingDirectory", d.WorkingDir)
	xmlKeyString(&b, "ProcessType", "Standard")
	xmlBool(&b, "RunAtLoad", true)
	xmlBool(&b, "KeepAlive", true)
	xmlInt(&b, "ThrottleInterval", 5)
	xmlInt(&b, "ExitTimeOut", 35)
	xmlKeyString(&b, "Umask", "077")
	xmlKeyString(&b, "StandardOutPath", d.LogPath)
	xmlKeyString(&b, "StandardErrorPath", d.LogPath)
	b.WriteString("</dict>\n</plist>\n")
	var doc any
	if err := xml.Unmarshal(b.Bytes(), &doc); err != nil {
		return "", fmt.Errorf("validate rendered plist XML: %w", err)
	}
	return b.String(), nil
}

var (
	createServiceTempFile    = createServiceTemp
	syncServiceDirectoryFile = syncServiceDirectory
	renameServiceFile        = func(root *os.Root, oldName, newName string) error { return root.Rename(oldName, newName) }
	writeServiceFile         = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
)

// writeFileSynced atomically replaces a regular file, never through a symlink.
func writeFileSynced(path string, data []byte) (err error) {
	dir, base := filepath.Dir(path), filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return errors.New("invalid definition path")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	if info, err := root.Lstat(base); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refuse non-regular definition %s", path)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := createServiceTempFile(root, base)
	if err != nil {
		return err
	}
	f := tmp.file
	tmpName := tmp.name
	ok := false
	defer func() {
		if !ok {
			err = errors.Join(err, root.Remove(tmpName))
		}
	}()
	if _, err = writeServiceFile(f, data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Chmod(tmpName, 0o600)
	}
	if err == nil {
		err = renameServiceFile(root, tmpName, base)
	}
	if err == nil {
		ok = true
		err = syncServiceDirectoryFile(root)
	}
	return err
}

type serviceTemp struct {
	file *os.File
	name string
}

func createServiceTemp(root *os.Root, base string) (serviceTemp, error) {
	for range 100 {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return serviceTemp{}, err
		}
		name := "." + base + ".tmp-" + hex.EncodeToString(random[:])
		f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return serviceTemp{file: f, name: name}, err
	}
	return serviceTemp{}, errors.New("cannot allocate unique definition temporary file")
}

func syncServiceDirectory(root *os.Root) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil && runtime.GOOS == goOSWindows && (errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, os.ErrPermission)) {
		err = nil
	}
	if err != nil && (errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP)) {
		err = nil
	}
	return errors.Join(err, closeErr)
}

// plistXML is the product plist decoder. It lives in a non-test file because
// launchdDefinitionProgram reads Program from an installed definition, and
// go build does not compile *_test.go.
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

// plistXMLValue is one top-level dict value. Type is the element name, so a
// boolean is "true" or "false" rather than text.
type plistXMLValue struct {
	Type, Text string
	Items      []string
}

// decodePlistDict pairs each key with the value in the same position. A dict
// whose counts differ is an error: indexing the shorter side would return
// another key's program text.
func decodePlistDict(data []byte) (map[string]plistXMLValue, error) {
	var p plistXML
	if err := xml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse launchd definition: %w", err)
	}
	if len(p.Dict.Keys) != len(p.Dict.Values) {
		return nil, fmt.Errorf("parse launchd definition: %d keys and %d values", len(p.Dict.Keys), len(p.Dict.Values))
	}
	out := make(map[string]plistXMLValue, len(p.Dict.Keys))
	for i, key := range p.Dict.Keys {
		v := p.Dict.Values[i]
		out[key] = plistXMLValue{Type: v.XMLName.Local, Text: strings.TrimSpace(v.Text), Items: v.Items}
	}
	return out, nil
}
