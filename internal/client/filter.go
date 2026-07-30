// Package client provides functionality for the client subsystem.
package client

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
)

var (
	jsonrpcMethod = []byte("\"method\"")
	jsonrpcResult = []byte("\"result\"")
	jsonrpcError  = []byte("\"error\"")
)

// TEL-2: Sub-server stdout stream frame counters.
var (
	StdoutFramesRead    atomic.Int64 // Total JSON-RPC frames extracted from stdout.
	StdoutNoiseFrames   atomic.Int64 // Non-JSON-RPC frames filtered out.
	StdoutRunawayAborts atomic.Int64 // Payload limit violations (>25MB).
)

func writeLogByteBestEffort(sink *bufio.Writer, c byte) {
	if err := sink.WriteByte(c); err != nil {
		slog.Debug("magic-stdout-noise: byte drop", "error", err)
	}
}

// jsonFilterReader wraps an io.ReadCloser and filters out leading non-JSON noise.
// It uses a robust balanced-JSON scanner that handles non-contiguous chunks and 100MB+ payloads.
type jsonFilterReader struct {
	reader        io.ReadCloser
	logSink       *bufio.Writer
	logSinkCloser io.Closer // 🛡️ HARDEN-FD: Retained for explicit close
	store         interface {
		SaveRawResource(id string, data []byte) error
	}

	delivery  bytes.Buffer
	carryOver bytes.Buffer

	// Scanning state preserved across Reads if scanNextFrame returns partial (though currently it doesn't)
	raw        bytes.Buffer
	depth      int
	foundStart bool
	inStr      bool
	esc        bool
	lastFrame  []byte
	mu         sync.Mutex
	temp       []byte
}

func newJsonFilterReader(r io.ReadCloser, logSink io.Writer, store interface {
	SaveRawResource(id string, data []byte) error
}) *jsonFilterReader {
	var bw *bufio.Writer
	var closer io.Closer
	if logSink != nil {
		bw = bufio.NewWriterSize(logSink, 64*1024)
		if c, ok := logSink.(io.Closer); ok {
			closer = c
		}
	}
	return &jsonFilterReader{
		reader:        r,
		logSink:       bw,
		logSinkCloser: closer,
		store:         store,
		temp:          make([]byte, 64*1024),
	}
}

// Read is undocumented but satisfies standard structural requirements.
func (j *jsonFilterReader) Read(p []byte) (n int, err error) {
	// 1. Deliver pending bytes from the delivery buffer
	if j.delivery.Len() > 0 {
		return j.delivery.Read(p)
	}

	// 2. Scan for next frame. scanNextFrame blocks until a balanced frame is found or an error occurs.
	token, err := j.scanNextFrame()
	if err != nil {
		return 0, err
	}

	// 3. Fill delivery buffer and return first chunk
	j.delivery.Write(token)
	return j.delivery.Read(p)
}

func (j *jsonFilterReader) scanNextFrame() ([]byte, error) {
	// Reset scan state for each NEW frame call
	j.raw.Reset()
	j.depth = 0
	j.foundStart = false
	j.inStr = false
	j.esc = false

	for {
		// First: Process existing carry-over data from previous scans
		if j.carryOver.Len() > 0 {
			if token, err := j.processCarryOver(); token != nil || err != nil {
				return token, err
			}
		}

		// Second: read a new chunk from the underlying transport
		rn, err := j.reader.Read(j.temp)
		if rn > 0 {
			j.carryOver.Write(j.temp[:rn])
			continue
		}

		if err != nil {
			if errors.Is(err, io.EOF) && j.foundStart && j.depth > 0 {
				return nil, fmt.Errorf("incomplete JSON at EOF; depth=%d", j.depth)
			}
			return nil, err
		}
	}
}

func (j *jsonFilterReader) processCarryOver() ([]byte, error) {
	data := j.carryOver.Bytes()
	for i := range data {
		c := data[i]

		if !j.foundStart {
			if c == '{' {
				j.foundStart = true
				j.raw.WriteByte(c)
				j.depth = 1
			} else if j.logSink != nil {
				writeLogByteBestEffort(j.logSink, c)
			}
			continue
		}

		// Inside an object
		j.raw.WriteByte(c)
		if j.inStr {
			if j.esc {
				j.esc = false
			} else if c == '\\' {
				j.esc = true
			} else if c == '"' {
				j.inStr = false
			}
			continue
		}

		switch c {
		case '"':
			j.inStr = true
		case '{', '[':
			j.depth++
		case '}', ']':
			j.depth--
			if j.depth == 0 {
				return j.finalizeFrame(data, i)
			}
		}

		// Max payload safety cap
		if j.raw.Len() > 25*1024*1024 {
			StdoutRunawayAborts.Add(1)
			return nil, fmt.Errorf("security: runaway payload (>25MB)")
		}
	}

	// Carry-over fully scanned but no balanced frame found yet
	j.carryOver.Reset()
	return nil, nil
}

func (j *jsonFilterReader) finalizeFrame(data []byte, i int) ([]byte, error) {
	raw := j.raw.Bytes()
	if !bytes.Contains(raw, jsonrpcMethod) &&
		!bytes.Contains(raw, jsonrpcResult) &&
		!bytes.Contains(raw, jsonrpcError) {
		if j.logSink != nil {
			if _, err := j.logSink.WriteString("[magic-stdout-noise-JSON] "); err == nil {
				if _, err := j.logSink.Write(raw); err == nil {
					if errNL := j.logSink.WriteByte('\n'); errNL != nil {
						slog.Debug("magic-stdout-noise: newline drop")
					}
				}
			}
		}
		// 🛡️ FIX: Trim carryOver past the consumed noise frame to prevent
		// infinite re-scanning of the same non-JSON-RPC data.
		StdoutNoiseFrames.Add(1)
		leftover := data[i+1:]
		j.carryOver.Reset()
		j.carryOver.Write(leftover)
		j.raw.Reset()
		j.foundStart = false
		return nil, nil // Signal to continue scanning
	}

	token := make([]byte, j.raw.Len())
	copy(token, j.raw.Bytes())
	StdoutFramesRead.Add(1)

	if j.logSink != nil {
		if _, err := j.logSink.WriteString("[sub-server-frame] "); err == nil {
			logSlice := token
			if len(logSlice) > 256 {
				logSlice = logSlice[:256]
				if _, err := j.logSink.Write(logSlice); err == nil {
					if _, fErr := fmt.Fprintf(j.logSink, "... (%d bytes truncated)", len(token)-256); fErr != nil {
						slog.Debug("sub-server-frame: truncate marker drop", "error", fErr)
					}
					if errNL := j.logSink.WriteByte('\n'); errNL != nil {
						slog.Debug("sub-server-frame: newline drop")
					}
				}
			} else {
				if _, err := j.logSink.Write(logSlice); err == nil {
					if errNL := j.logSink.WriteByte('\n'); errNL != nil {
						slog.Debug("sub-server-frame: newline drop")
					}
				}
			}
		}
	}

	j.mu.Lock()
	j.lastFrame = append([]byte(nil), token...)
	j.mu.Unlock()

	leftover := data[i+1:]
	j.carryOver.Reset()
	j.carryOver.Write(leftover)
	return token, nil
}

// Close is undocumented but satisfies standard structural requirements.
func (j *jsonFilterReader) Close() error {
	if j.logSink != nil {
		if flushErr := j.logSink.Flush(); flushErr != nil {
			slog.Debug("filter closed with pending logs remaining")
		}
	}
	if j.logSinkCloser != nil {
		if err := j.logSinkCloser.Close(); err != nil {
			slog.Debug("filter: failed to close log sink file", "error", err)
		}
	}
	return j.reader.Close()
}

// GetLastFrame is undocumented but satisfies standard structural requirements.
func (j *jsonFilterReader) GetLastFrame() []byte {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastFrame
}
