package handler

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveGoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SDK lookup does not apply on Windows; amendment (a) leaves that platform on PATH")
	}

	t.Run("newest wins when text order is oldest first", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"go1.26.11", "go1.27.4"},
			{"go1.26.1", "go1.27.4"},
			{"go1.1.3", "go1.10.5"},
		} {
			home := t.TempDir()
			touchSDK(t, home, pair[0], 0o755)
			want := touchSDK(t, home, pair[1], 0o755)
			got := resolveGoBinary("", home, nil)
			if got != want {
				t.Errorf("%s with %s: got %s, want %s", pair[0], pair[1], got, want)
			}
		}
	})

	t.Run("text order that already matches version still picks newest", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "go1.26.9", 0o755)
		want := touchSDK(t, home, "go1.26.11", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
		home = t.TempDir()
		touchSDK(t, home, "go1.9.0", 0o755)
		want = touchSDK(t, home, "go1.27.4", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("release wins over release candidate", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "go1.27rc1", 0o755)
		want := touchSDK(t, home, "go1.27.4", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("release candidate does not outrank an older release", func(t *testing.T) {
		home := t.TempDir()
		want := touchSDK(t, home, "go1.26.11", 0o755)
		touchSDK(t, home, "go1.27rc1", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("unparsed names alone use descending text", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "go-old", 0o755)
		touchSDK(t, home, "go1.27rc1", 0o755)
		want := touchSDK(t, home, "gotip", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("parsable name wins over unparsed", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "gotip", 0o755)
		want := touchSDK(t, home, "go1.26.1", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("equal versions tie by ascending path", func(t *testing.T) {
		home := t.TempDir()
		shorter := touchSDK(t, home, "go1.27", 0o755)
		longer := touchSDK(t, home, "go1.27.0", 0o755)
		want := shorter
		if longer < shorter {
			want = longer
		}
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("non-executable newest is skipped", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "go1.27.4", 0o644)
		want := touchSDK(t, home, "go1.26.11", 0o755)
		if got := resolveGoBinary("", home, nil); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	})

	t.Run("no SDK uses the first executable fallback then go", func(t *testing.T) {
		home := t.TempDir()
		bin := filepath.Join(home, "fallback", "go")
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := resolveGoBinary("", home, []string{bin}); got != bin {
			t.Fatalf("got %s, want %s", got, bin)
		}
		if got := resolveGoBinary("", home, []string{filepath.Join(home, "missing", "go")}); got != "go" {
			t.Fatalf("got %s, want go", got)
		}
	})

	t.Run("override wins unchecked", func(t *testing.T) {
		home := t.TempDir()
		touchSDK(t, home, "go1.27.4", 0o755)
		if got := resolveGoBinary("/not/a/real/go", home, nil); got != "/not/a/real/go" {
			t.Fatalf("got %s, want the override", got)
		}
	})
}

func TestFixedGoFallbacksOmitStaleNames(t *testing.T) {
	for _, p := range fixedGoFallbacks("/home/example") {
		if strings.Contains(p, "go1.26.5") || strings.Contains(p, "go1.26.1") {
			t.Fatalf("stale SDK name in fallbacks: %s", p)
		}
	}
	src, err := os.ReadFile("execute_pipeline.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if strings.Contains(text, "go1.26.5") || strings.Contains(text, "go1.26.1") {
		t.Fatal("stale SDK names still passed by mcpGoBinary")
	}
}

func touchSDK(t *testing.T, home, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(home, "sdk", name, "bin", "go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}
