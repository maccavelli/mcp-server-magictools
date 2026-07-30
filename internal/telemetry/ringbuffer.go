//go:build !android

package telemetry

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/blevesearch/mmap-go"
)

const (
	RingTotalSize      = 128 * 1024 * 1024 // 128MB
	RingHeaderSize     = 64 * 1024         // 64KB header (gauge snapshot area)
	RingPayloadSize    = RingTotalSize - RingHeaderSize
	gaugeLengthOffset  = 16
	gaugePayloadOffset = 20
)

// RingBuffer provides memory-mapped circular logging.
type RingBuffer struct {
	file *os.File
	mmap mmap.MMap
	head atomic.Uint64
	mu   sync.Mutex // Serializes writers only
}

// GlobalRingBuffer holds the singleton process ring buffer.
var GlobalRingBuffer *RingBuffer

// MustInitializeRingBuffer creates and maps the shadow channel.
func MustInitializeRingBuffer(filename string) {
	rb, err := NewRingBuffer(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[telemetry] Warning: Failed to initialize mmap ring buffer: %v\n", err)
		return
	}
	GlobalRingBuffer = rb
}

// NewRingBuffer opens or creates the mmap file.
func NewRingBuffer(path string) (*RingBuffer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // G301: user-local telemetry mmap directory
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) //nolint:gosec // G302: user-local telemetry mmap file
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		closeFileOrWarn(file, "ringbuffer-stat")
		return nil, err
	}

	if info.Size() != int64(RingTotalSize) {
		if err := file.Truncate(int64(RingTotalSize)); err != nil {
			closeFileOrWarn(file, "ringbuffer-truncate")
			return nil, err
		}
		// Write magic bytes
		var header [RingHeaderSize]byte
		copy(header[0:4], "RING")
		binary.LittleEndian.PutUint32(header[4:8], 1)  // Version
		binary.LittleEndian.PutUint64(header[8:16], 0) // Head starts at 0

		if _, err := file.WriteAt(header[:], 0); err != nil {
			closeFileOrWarn(file, "ringbuffer-header")
			return nil, err
		}
	}

	m, err := mmap.Map(file, mmap.RDWR, 0)
	if err != nil {
		closeFileOrWarn(file, "ringbuffer-mmap")
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	// Read initial head position securely
	headPos := binary.LittleEndian.Uint64(m[8:16])
	if headPos >= uint64(RingPayloadSize) {
		headPos = 0
		binary.LittleEndian.PutUint64(m[8:16], 0)
	}

	rb := &RingBuffer{
		file: file,
		mmap: m,
	}
	rb.head.Store(headPos)
	return rb, nil
}

// WriteGauges updates the header with a length-prefixed JSON snapshot.
func (r *RingBuffer) WriteGauges(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	maxPayload := RingHeaderSize - gaugePayloadOffset
	if len(b) > maxPayload {
		return fmt.Errorf("gauge payload exceeds %dKB limit", maxPayload/1024)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for i := gaugePayloadOffset; i < RingHeaderSize; i++ {
		r.mmap[i] = 0
	}
	copy(r.mmap[gaugePayloadOffset:], b)
	binary.LittleEndian.PutUint32(r.mmap[gaugeLengthOffset:gaugePayloadOffset], safeUint32FromInt(len(b)))
	return nil
}

// WriteRecord appends a delimited log payload to the ring buffer.
func (r *RingBuffer) WriteRecord(record []byte) {
	if len(record) == 0 {
		return
	}

	// Payload with newline boundary
	payload := make([]byte, len(record)+1)
	copy(payload, record)
	payload[len(record)] = '\n'
	sz := uint64(len(payload))

	// Prevent RingBuffer mathematical underflow from payload poisoning
	if sz > uint64(RingPayloadSize) {
		truncated := []byte("{\"level\":\"ERROR\",\"msg\":\"log payload truncated: exceeded hardware telemetry ring buffer limit\"}")
		payload = make([]byte, len(truncated)+1)
		copy(payload, truncated)
		payload[len(truncated)] = '\n'
		sz = uint64(len(payload))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	h := r.head.Load()
	if h+sz <= uint64(RingPayloadSize) {
		// Contiguous write
		copy(r.mmap[RingHeaderSize+h:], payload)
		binary.LittleEndian.PutUint64(r.mmap[8:16], h+sz)
		r.head.Store(h + sz)
	} else {
		// Circular write
		chunk1 := uint64(RingPayloadSize) - h
		copy(r.mmap[RingHeaderSize+h:], payload[:chunk1])
		chunk2 := sz - chunk1
		copy(r.mmap[RingHeaderSize:], payload[chunk1:])
		binary.LittleEndian.PutUint64(r.mmap[8:16], chunk2)
		r.head.Store(chunk2)
	}
}

// ReadState provides a snapshot copy of the ring buffer for the dashboard.
// Returns nil, nil, nil when the ring file doesn't exist or hasn't been
// initialized yet (size != RingTotalSize). Callers should treat nil gauge/log
// slices as "no data available" rather than an error.
func ReadState(path string) ([]byte, []byte, error) {
	file, err := os.Open(filepath.Clean(path)) //nolint:gosec // G304: path from controlled telemetry directory
	if err != nil {
		return nil, nil, nil // File not created yet — return empty state
	}
	defer closeFileOrWarn(file, "ringbuffer-read")

	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat failed: %w", err)
	}
	if info.Size() != int64(RingTotalSize) {
		return nil, nil, nil // Ring file not yet initialized — return empty state
	}

	m, err := mmap.Map(file, mmap.RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer unmapOrWarn(m)

	// Extract length-prefixed gauge JSON (v2), with legacy null-terminated fallback.
	var gaugeBytes []byte
	payloadLen := binary.LittleEndian.Uint32(m[gaugeLengthOffset:gaugePayloadOffset])
	maxPayload := uint32(RingHeaderSize - gaugePayloadOffset)
	if payloadLen > 0 && payloadLen <= maxPayload {
		gaugeBytes = make([]byte, payloadLen)
		copy(gaugeBytes, m[gaugePayloadOffset:gaugePayloadOffset+payloadLen])
	} else {
		legacy := make([]byte, RingHeaderSize-gaugeLengthOffset)
		copy(legacy, m[gaugeLengthOffset:RingHeaderSize])
		nullIdx := -1
		for i, b := range legacy {
			if b == 0 {
				nullIdx = i
				break
			}
		}
		if nullIdx != -1 {
			legacy = legacy[:nullIdx]
		}
		gaugeBytes = legacy
	}

	// Extract circular log payload correctly wrapping from head
	headPos := binary.LittleEndian.Uint64(m[8:16])
	logBytes := make([]byte, RingPayloadSize)

	// Copy oldest first (head to end), then newest (start to head)
	oldestChunk := m[RingHeaderSize+headPos : RingTotalSize]
	newestChunk := m[RingHeaderSize : RingHeaderSize+headPos]

	copy(logBytes, oldestChunk)
	copy(logBytes[len(oldestChunk):], newestChunk)

	return gaugeBytes, logBytes, nil
}
