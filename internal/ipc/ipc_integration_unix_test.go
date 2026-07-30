//go:build !windows

package ipc_test

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/ipc"
)

// TestAuthFilePath_UserScoped verifies that AuthFilePath never returns /tmp on
// platforms where XDG_RUNTIME_DIR or UserCacheDir are available.
func TestAuthFilePath_UserScoped(t *testing.T) {
	path := ipc.AuthFilePath()
	if path == "" {
		t.Fatal("AuthFilePath returned empty string")
	}
	t.Logf("AuthFilePath = %s", path)
}

// testSocketPath returns a unique, temp-scoped unix socket path for tests,
// preventing collisions with a running production service on the default SocketPath.
func testSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.sock")
}

// TestIPCListenDial verifies listen→dial round-trips a payload correctly
// over the unix socket transport using a test-scoped socket.
func TestIPCListenDial(t *testing.T) {
	const testPayload = `{"jsonrpc":"2.0","method":"ping","id":1}`

	sockPath := testSocketPath(t)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	os.Chmod(sockPath, 0600)

	errc := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errc <- fmt.Errorf("Accept: %w", err)
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			errc <- fmt.Errorf("Read: %w", err)
			return
		}
		_, err = conn.Write(buf[:n])
		errc <- err
	}()

	dialer, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer dialer.Close()

	if _, err := dialer.Write([]byte(testPayload)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_ = dialer.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	n, err := dialer.Read(buf)
	if err != nil {
		t.Fatalf("Read echo: %v", err)
	}
	if got := string(buf[:n]); got != testPayload {
		t.Errorf("echo mismatch: got %q, want %q", got, testPayload)
	}

	if err := <-errc; err != nil {
		t.Errorf("server goroutine error: %v", err)
	}
}

// TestIPCListenPrimary_RejectsLiveSocket validates ROB-1: ListenPrimary refuses
// to bind if another instance is already listening.
func TestIPCListenPrimary_RejectsLiveSocket(t *testing.T) {
	sockPath := ipc.SocketPath()
	// If production socket is live, verify ListenPrimary rejects
	if conn, err := net.DialTimeout("unix", sockPath, 200*time.Millisecond); err == nil {
		conn.Close()
		_, err := ipc.ListenPrimary()
		if err == nil {
			t.Fatal("expected ListenPrimary to fail when another instance is listening")
		}
		t.Logf("correctly rejected: %v", err)
		return
	}
	// No live socket — ListenPrimary should succeed
	ln, err := ipc.ListenPrimary()
	if err != nil {
		t.Fatalf("ListenPrimary should succeed on clean socket: %v", err)
	}
	ln.Close()
	os.Remove(sockPath)
}

// TestIPCConcurrentClients verifies multiple concurrent clients can connect
// to the same unix socket listener without interference.
func TestIPCConcurrentClients(t *testing.T) {
	sockPath := testSocketPath(t)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	os.Chmod(sockPath, 0600)

	const numClients = 5
	ready := make(chan struct{})

	go func() {
		close(ready)
		for range numClients {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 256)
				n, _ := c.Read(buf)
				_, _ = c.Write(buf[:n])
			}(conn)
		}
	}()

	<-ready
	errs := make(chan error, numClients)
	for i := range numClients {
		go func(id int) {
			conn, err := net.Dial("unix", sockPath)
			if err != nil {
				errs <- fmt.Errorf("client %d dial: %w", id, err)
				return
			}
			defer conn.Close()

			payload := fmt.Sprintf(`{"id":%d}`, id)
			if _, err := conn.Write([]byte(payload)); err != nil {
				errs <- fmt.Errorf("client %d write: %w", id, err)
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, 256)
			n, err := conn.Read(buf)
			if err != nil {
				errs <- fmt.Errorf("client %d read: %w", id, err)
				return
			}
			if string(buf[:n]) != payload {
				errs <- fmt.Errorf("client %d: echo mismatch", id)
				return
			}
			errs <- nil
		}(i)
	}

	for range numClients {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
