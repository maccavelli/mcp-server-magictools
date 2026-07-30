package util

import (
	"encoding/json"
	"testing"
)

func TestSqueezeResult(t *testing.T) {
	res := SqueezeResult(json.RawMessage(`{"a": "b"}`))
	b, _ := json.Marshal(res)
	if string(b) != `{"a":"b"}` {
		t.Errorf("expected squeezed, got %s", b)
	}
	res2 := SqueezeResult(nil)
	if res2 != nil {
		t.Error("expected nil")
	}
}

func TestMarshalMinified(t *testing.T) {
	b, err := MarshalMinified(map[string]string{"a": "b"})
	if err != nil {
		t.Error(err)
	}
	if string(b) != `{"a":"b"}` {
		t.Error("marshal minified fail")
	}
}
