package logutil

import (
	"os"
	"reflect"
	"testing"
)

func TestTailFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "tail_test_*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	tests := []struct {
		name     string
		lines    int
		expected []string
	}{
		{
			name:     "tail 2 lines",
			lines:    2,
			expected: []string{"line9", "line10"},
		},
		{
			name:     "tail 5 lines",
			lines:    5,
			expected: []string{"line6", "line7", "line8", "line9", "line10"},
		},
		{
			name:     "tail more lines than exist",
			lines:    20,
			expected: []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10"},
		},
		{
			name:     "tail 0 lines",
			lines:    0,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TailFile(tmpFile.Name(), tt.lines)
			if err != nil {
				t.Errorf("TailFile() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("TailFile() = %v, want %v", got, tt.expected)
			}
		})
	}
}
