package ipc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type AuthFile struct {
	Port     int    `json:"port"`
	Port6    int    `json:"port6"`
	Token    string `json:"token"`
	LLMPort  int    `json:"llm_port,omitempty,omitzero"`
	LLMToken string `json:"llm_token,omitempty,omitzero"`
}

// AuthFilePath returns the path to the auth token file.
// Priority order:
//  1. $XDG_RUNTIME_DIR — kernel-managed 0700 tmpfs on systemd Linux (most secure)
//  2. os.UserCacheDir() — ~/Library/Caches on macOS, %LOCALAPPDATA% on Windows
//  3. os.TempDir()     — fallback for non-systemd Linux and unusual environments
//
// The file is always written with 0600 permissions (owner-readable only).
func AuthFilePath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "magictools_auth.json")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		if mkErr := os.MkdirAll(dir, 0700); mkErr != nil {
			return filepath.Join(os.TempDir(), "magictools_auth.json")
		}
		return filepath.Join(dir, "magictools_auth.json")
	}
	return filepath.Join(os.TempDir(), "magictools_auth.json")
}

func GenerateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed — cannot generate secure IPC token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func WriteAuthFileAtomic(auth AuthFile) error {
	path := AuthFilePath()
	tmpPath := path + ".tmp"

	data, err := json.Marshal(auth)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Clean(tmpPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600) //nolint:gosec // tmp path derived from controlled auth file location
	if err != nil {
		return err
	}

	if _, err := f.Write(data); err != nil {
		return closeAuthTempFile(f, tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		return closeAuthTempFile(f, tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return closeAuthTempFile(nil, tmpPath, err)
	}

	return os.Rename(tmpPath, path)
}

func ReadAuthFile() (AuthFile, error) {
	var auth AuthFile
	data, err := os.ReadFile(AuthFilePath())
	if err != nil {
		return auth, err
	}
	err = json.Unmarshal(data, &auth)
	return auth, err
}

func ListenDualStackTCP() ([]net.Listener, int, int, error) {
	var listeners []net.Listener
	var port4, port6 int

	// Try IPv4 127.0.0.1
	l4, err4 := net.Listen("tcp", "127.0.0.1:0")
	if err4 == nil {
		listeners = append(listeners, l4)
		if addr, ok := l4.Addr().(*net.TCPAddr); ok {
			port4 = addr.Port
		}
	}

	// Try IPv6 [::1]
	l6, err6 := net.Listen("tcp", "[::1]:0")
	if err6 == nil {
		listeners = append(listeners, l6)
		if addr, ok := l6.Addr().(*net.TCPAddr); ok {
			port6 = addr.Port
		}
	}

	if len(listeners) == 0 {
		return nil, 0, 0, fmt.Errorf("failed to bind any TCP fallback ports: ipv4=%w, ipv6=%w", err4, err6)
	}

	return listeners, port4, port6, nil
}

func DialTCPFallback() (net.Conn, string, error) {
	auth, err := ReadAuthFile()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read auth file: %w", err)
	}

	var conn net.Conn
	var dialErr error

	if auth.Port > 0 {
		conn, dialErr = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", auth.Port))
		if dialErr == nil {
			return conn, auth.Token, nil
		}
	}

	if auth.Port6 > 0 {
		conn6, dialErr6 := net.Dial("tcp", fmt.Sprintf("[::1]:%d", auth.Port6))
		if dialErr6 == nil {
			return conn6, auth.Token, nil
		}
		dialErr = fmt.Errorf("ipv4 err: %w, ipv6 err: %w", dialErr, dialErr6)
	}

	return nil, "", fmt.Errorf("tcp fallback dial failed: %w", dialErr)
}

func closeAuthTempFile(f *os.File, tmpPath string, err error) error {
	if f != nil {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if removeErr := os.Remove(tmpPath); removeErr != nil && err == nil {
		err = removeErr
	}
	return err
}
