package util

import (
	"os"
	"testing"
)

func TestMatchEnviron(t *testing.T) {
	data := []byte("KEY1=VALUE1\x00KEY2=VALUE2\x00KEY3=VALUE3\x00")

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"Exact match", "KEY1=VALUE1", true},
		{"Middle match", "KEY2=VALUE2", true},
		{"Last match", "KEY3=VALUE3", true},
		{"Prefix substring", "KEY1", false},
		{"Value only", "VALUE1", false},
		{"Non-existent", "KEY4=VALUE4", false},
		{"Partial key", "KEY", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchEnviron(data, tt.target); got != tt.want {
				t.Errorf("MatchEnviron() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchEnvironPrefix(t *testing.T) {
	data := []byte("SERVER=github\x00HOST=localhost\x00")

	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{"Exact prefix", "SERVER=", true},
		{"Partial prefix", "SER", true},
		{"Full match", "SERVER=github", true},
		{"No match", "GIT", false},
		{"Value substring", "hub", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchEnvironPrefix(data, tt.prefix); got != tt.want {
				t.Errorf("MatchEnvironPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestGetRSS(t *testing.T) {
	pid := os.Getpid()
	rss, err := GetRSS(pid)
	if err != nil {
		t.Fatalf("GetRSS failed: %v", err)
	}
	if rss == 0 {
		t.Errorf("GetRSS returned 0")
	}
}

func TestGetProcessCPU(t *testing.T) {
	pid := os.Getpid()
	_, err := GetProcessCPU(pid)
	if err != nil {
		t.Fatalf("GetProcessCPU failed: %v", err)
	}
}

func TestGetProcessEnviron(t *testing.T) {
	pid := os.Getpid()
	env, err := GetProcessEnviron(pid)
	if err != nil {
		t.Fatalf("GetProcessEnviron failed: %v", err)
	}
	if len(env) == 0 {
		t.Errorf("GetProcessEnviron returned empty slice")
	}
}
