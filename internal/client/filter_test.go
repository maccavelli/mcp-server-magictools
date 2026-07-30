package client

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestJsonFilterReader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		noise    string
	}{
		{
			name:     "Clean JSON-RPC",
			input:    `{"jsonrpc":"2.0","method":"ping"}`,
			expected: `{"jsonrpc":"2.0","method":"ping"}`,
		},
		{
			name:     "Leading noise",
			input:    `some noise {"jsonrpc":"2.0","method":"ping"}`,
			expected: `{"jsonrpc":"2.0","method":"ping"}`,
			noise:    "some noise ",
		},
		{
			name:     "Multi-keyword: method only",
			input:    `{"method":"notifications/initialized"}`,
			expected: `{"method":"notifications/initialized"}`,
		},
		{
			name:     "Multi-keyword: result only",
			input:    `{"result":{"tools":[]}}`,
			expected: `{"result":{"tools":[]}}`,
		},
		{
			name:     "Multi-keyword: error only",
			input:    `{"error":{"code":-32601,"message":"Method not found"}}`,
			expected: `{"error":{"code":-32601,"message":"Method not found"}}`,
		},
		{
			name:     "Junk JSON shunted to noise",
			input:    `{"level":"info","msg":"Sub-server starting"}`,
			expected: "",
			noise:    "[magic-stdout-noise-JSON] {\"level\":\"info\",\"msg\":\"Sub-server starting\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logSink bytes.Buffer
			input := io.NopCloser(strings.NewReader(tt.input))
			filter := newJsonFilterReader(input, &logSink, nil)

			output, err := io.ReadAll(filter)
			if err != nil && !errors.Is(err, io.EOF) {
				t.Fatalf("Read error: %v", err)
			}

			// Flush the log sync via Close
			filter.Close()

			if string(output) != tt.expected {
				t.Errorf("Expected output %q, got %q", tt.expected, string(output))
			}

			if tt.noise != "" && !strings.Contains(logSink.String(), tt.noise) {
				t.Errorf("Expected noise %q, got %q", tt.noise, logSink.String())
			}
		})
	}
}
func TestFilterGetLastFrame(t *testing.T) {
	inputStr := `{"jsonrpc":"2.0","result":"ok"}`
	input := io.NopCloser(strings.NewReader(inputStr))
	var logSink bytes.Buffer
	filter := newJsonFilterReader(input, &logSink, nil)

	_, _ = io.ReadAll(filter)

	last := filter.GetLastFrame()
	if string(last) != inputStr {
		t.Errorf("GetLastFrame() = %q; want %q", string(last), inputStr)
	}
}
