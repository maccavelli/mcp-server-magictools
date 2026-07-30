package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuthFilePath(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	// test XDG
	tmpXDG := t.TempDir()
	os.Setenv("XDG_RUNTIME_DIR", tmpXDG)
	if p := AuthFilePath(); p != filepath.Join(tmpXDG, "magictools_auth.json") {
		t.Errorf("unexpected path for XDG: %s", p)
	}

	// test without XDG
	os.Setenv("XDG_RUNTIME_DIR", "")
	p := AuthFilePath()
	if filepath.Base(p) != "magictools_auth.json" {
		t.Errorf("unexpected fallback path: %s", p)
	}
}

func TestGenerateToken(t *testing.T) {
	token1 := GenerateToken()
	token2 := GenerateToken()
	if len(token1) != 64 || len(token2) != 64 {
		t.Errorf("expected 64 chars hex token")
	}
	if token1 == token2 {
		t.Errorf("expected random tokens")
	}
}

func TestAuthFileReadWrite(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	tmpXDG := t.TempDir()
	os.Setenv("XDG_RUNTIME_DIR", tmpXDG)

	auth := AuthFile{
		Port:  1234,
		Port6: 5678,
		Token: "test-token",
	}

	err := WriteAuthFileAtomic(auth)
	if err != nil {
		t.Fatalf("WriteAuthFileAtomic failed: %v", err)
	}

	readAuth, err := ReadAuthFile()
	if err != nil {
		t.Fatalf("ReadAuthFile failed: %v", err)
	}

	if readAuth.Port != auth.Port || readAuth.Token != auth.Token {
		t.Errorf("mismatch read auth")
	}
}

func TestTCPFallbackLifecycle(t *testing.T) {
	origXDG := os.Getenv("XDG_RUNTIME_DIR")
	defer os.Setenv("XDG_RUNTIME_DIR", origXDG)

	tmpXDG := t.TempDir()
	os.Setenv("XDG_RUNTIME_DIR", tmpXDG)

	listeners, port4, port6, err := ListenDualStackTCP()
	if err != nil {
		t.Fatalf("ListenDualStackTCP failed: %v", err)
	}
	if len(listeners) == 0 {
		t.Fatalf("no listeners")
	}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	auth := AuthFile{
		Port:  port4,
		Port6: port6,
		Token: "test-conn-token",
	}
	err = WriteAuthFileAtomic(auth)
	if err != nil {
		t.Fatalf("WriteAuthFileAtomic failed: %v", err)
	}

	conn, token, err := DialTCPFallback()
	if err != nil {
		t.Fatalf("DialTCPFallback failed: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected conn")
	}
	defer conn.Close()

	if token != "test-conn-token" {
		t.Errorf("mismatch token: %s", token)
	}
}
