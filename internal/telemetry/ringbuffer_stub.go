//go:build android

package telemetry

// RingBuffer provides memory-mapped circular logging (NO-OP STUB).
type RingBuffer struct{}

// GlobalRingBuffer holds a stub nil reference.
var GlobalRingBuffer *RingBuffer

// MustInitializeRingBuffer is a no-op on unsupported platforms.
func MustInitializeRingBuffer(filename string) {
	GlobalRingBuffer = &RingBuffer{}
}

// WriteGauges is a no-op.
func (r *RingBuffer) WriteGauges(v any) error {
	return nil
}

// WriteRecord is a no-op.
func (r *RingBuffer) WriteRecord(record []byte) {
}

// ReadState provides a stub interface for dashboard logic without failing at compile time.
func ReadState(path string) ([]byte, []byte, error) {
	return nil, nil, nil
}
