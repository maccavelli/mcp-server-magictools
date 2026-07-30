package util

import (
	"sync"
)

const BufferSize = 64 * 1024 // 64KB high-density buffer for Warm Pool stability

var bytePool = sync.Pool{
	New: func() any {
		b := make([]byte, BufferSize)
		return &b
	},
}

// GetBuffer retrieves a 64KB buffer from the pool
func GetBuffer() *[]byte {
	raw := bytePool.Get()
	buf, ok := raw.(*[]byte)
	if !ok {
		b := make([]byte, BufferSize)
		return &b
	}
	return buf
}

// PutBuffer returns a buffer to the pool
func PutBuffer(b *[]byte) {
	if b == nil || cap(*b) < BufferSize {
		return // Don't pool smaller buffers
	}
	bytePool.Put(b)
}
