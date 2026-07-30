package logutil

import (
	"reflect"
	"testing"
)

func TestFilterLogs(t *testing.T) {
	lines := []string{
		"level=INFO msg=boot server=hub",
		"level=ERROR msg=fail server=github",
		"level=WARN msg=retry server=github",
		"{\"level\":\"ERROR\",\"server\":\"github\",\"msg\":\"boom\"}",
		"level=FATAL msg=panic server=core",
	}

	tests := []struct {
		name     string
		serverID string
		severity string
		expected []string
	}{
		{
			name:     "filter by server",
			serverID: "github",
			severity: "",
			expected: []string{
				"level=ERROR msg=fail server=github",
				"level=WARN msg=retry server=github",
				"{\"level\":\"ERROR\",\"server\":\"github\",\"msg\":\"boom\"}",
			},
		},
		{
			name:     "filter by server and error",
			serverID: "github",
			severity: "ERROR",
			expected: []string{
				"level=ERROR msg=fail server=github",
				"{\"level\":\"ERROR\",\"server\":\"github\",\"msg\":\"boom\"}",
			},
		},
		{
			name:     "filter by critical",
			serverID: "",
			severity: "CRITICAL",
			expected: []string{
				"level=FATAL msg=panic server=core",
			},
		},
		{
			name:     "no matches",
			serverID: "unknown",
			severity: "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterLogs(lines, tt.serverID, tt.severity)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("FilterLogs() = %v, want %v", got, tt.expected)
			}
		})
	}
}
