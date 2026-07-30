//go:build !windows

// Package client provides functionality for the client subsystem.
package client

import (
	"os"

	"golang.org/x/sys/unix"
)

// setCloexec sets the close-on-exec flag on the underlying file descriptor.
// Uses golang.org/x/sys/unix.CloseOnExec() for Go 1.26.5 idiom compliance
// and correct portability across Linux, macOS, FreeBSD, and other unix platforms.
// (The raw SYS_FCNTL approach used previously is fragile on non-Linux BSDs
// where syscall numbers differ.)
func setCloexec(w any) {
	if f, ok := w.(*os.File); ok {
		unix.CloseOnExec(int(f.Fd()))
	}
}
