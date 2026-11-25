//go:build !unix && !windows

package main

import (
	"context"
	"fmt"
	"runtime"
)

func acquireServiceLock(context.Context, string) (func(), error) {
	return nil, fmt.Errorf("service lock is not implemented on %s", runtime.GOOS)
}
