package util_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcplib"
)

func TestGetSource(t *testing.T) {
	ctx := context.Background()
	ctx = util.WithSource(ctx, "test-src")
	if util.GetSource(ctx) != "test-src" {
		t.Error("expected test-src")
	}
	if util.GetSource(context.Background()) != "" {
		t.Error("expected empty")
	}
}

func TestIsInternal(t *testing.T) {
	ctx := context.Background()
	util.TraceFunc(ctx, "test", func() error { return nil })
	// It relies on internal mechanics
}

func TestOpenHardenedLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	f := util.OpenHardenedLogFile(path)
	f.Close()

	content := bytes.Repeat([]byte("a"), 51*1024*1024)
	os.WriteFile(path, content, 0644)

	f2 := util.OpenHardenedLogFile(path)
	f2.Close()

	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Errorf("expected file to be truncated to 0, got size %d", info.Size())
	}
}

func TestOpenHardenedLogFile_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	// Nested path where the parent directory does NOT exist yet
	path := filepath.Join(dir, "deep", "nested", "test.log")

	f := util.OpenHardenedLogFile(path)
	if f == nil {
		t.Fatal("expected non-nil file handle when parent dir is missing")
	}
	defer f.Close()

	// Verify the file was actually created
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file to exist at %s: %v", path, err)
	}

	// Windows does not preserve Unix permission bits.
	if runtime.GOOS == "windows" {
		return
	}

	// Verify parent directory permissions are no broader than 0750. A restrictive
	// process umask may remove group bits from the requested mode.
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("expected parent dir to exist: %v", err)
	}
	perm := info.Mode().Perm()
	if perm&^os.FileMode(0750) != 0 {
		t.Errorf("expected parent dir perm no broader than 0750, got %04o", perm)
	}
}

func TestEofDetector(t *testing.T) {
	canceled := false
	cancel := func() { canceled = true }

	reader := strings.NewReader("hello")
	det := mcplib.NewEOFDetector(reader, cancel) //nolint:staticcheck // Covers compatibility behavior for deprecated helper.

	buf := make([]byte, 10)
	_, _ = det.Read(buf)
	if canceled {
		t.Error("expected not canceled yet")
	}

	_, _ = det.Read(buf) // Should hit EOF
	if !canceled {
		t.Error("expected cancel to be called on EOF")
	}
}

func TestAutoFlusher(t *testing.T) {
	var buf bytes.Buffer
	af := mcplib.NewAutoFlusher(&buf) //nolint:staticcheck // Covers compatibility behavior for deprecated helper.
	_, _ = af.Write([]byte("test"))

	if buf.String() != "test" {
		t.Error("expected test")
	}

	// Test with a flusher
	fw := &flushWriter{Buffer: &buf}
	af2 := mcplib.NewAutoFlusher(fw) //nolint:staticcheck // Covers compatibility behavior for deprecated helper.
	_, _ = af2.Write([]byte("test2\n"))
	if !fw.flushed {
		t.Error("expected Flush to be called")
	}
}

type flushWriter struct {
	*bytes.Buffer
	flushed bool
}

func (f *flushWriter) Flush() error {
	f.flushed = true
	return nil
}

func TestNopReadWriteCloser(t *testing.T) {
	rc := mcplib.NopReadCloser{Reader: strings.NewReader("test")}
	if err := rc.Close(); err != nil {
		t.Errorf("NopReadCloser.Close() error: %v", err)
	}

	wc := mcplib.NopWriteCloser{Writer: &bytes.Buffer{}}
	if err := wc.Close(); err != nil {
		t.Errorf("NopWriteCloser.Close() error: %v", err)
	}
}
