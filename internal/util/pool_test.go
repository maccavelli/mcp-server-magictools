package util

import (
	"testing"
)

func TestPool(t *testing.T) {
	b := GetBuffer()
	if b == nil {
		t.Error("got nil")
	}
	PutBuffer(b)
}
