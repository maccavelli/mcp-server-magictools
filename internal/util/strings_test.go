package util

import (
	"testing"
)

func TestGenerateSessionID(t *testing.T) {
	s1 := GenerateSessionID()
	s2 := GenerateSessionID()
	if s1 == s2 || len(s1) == 0 {
		t.Error("session ids should be unique and non-empty")
	}
}

func TestTruncateAllLargeStrings(t *testing.T) {
	str := string(make([]byte, 2000))
	res := TruncateAllLargeStrings(str, 50)
	if len(res.(string)) == 2000 {
		t.Error("truncate failed")
	}
}

func TestCenterTruncate(t *testing.T) {
	str := string(make([]byte, 2000))
	res := CenterTruncate(str, 5)
	if len(res) == 2000 {
		t.Error("truncate failed")
	}
}

func TestIsStackTrace(t *testing.T) {
	if !IsStackTrace("panic goroutine 1 [running]:") {
		t.Error("expected true")
	}
	if IsStackTrace("hello world") {
		t.Error("expected false")
	}
}

func TestTruncateAllLargeStringsMapAndSlice(t *testing.T) {
	str := string(make([]byte, 2000))
	m := map[string]any{"key1": str, "key2": []any{str}}
	res := TruncateAllLargeStrings(m, 50).(map[string]any)
	if len(res["key1"].(string)) == 2000 {
		t.Error("truncate map failed")
	}
	sliceData := res["key2"].([]any)
	if len(sliceData[0].(string)) == 2000 {
		t.Error("truncate array failed")
	}
}
