package backplane

import (
	"encoding/json"
	"testing"
)

func TestSmartID_UnmarshalJSON(t *testing.T) {
	var id1, id2, id3 SmartID

	if err := json.Unmarshal([]byte(`"abc"`), &id1); err != nil || id1.Val != "abc" {
		t.Errorf("failed string id")
	}
	if err := json.Unmarshal([]byte(`123`), &id2); err != nil || id2.Val != float64(123) {
		t.Errorf("failed numeric id")
	}
	if err := json.Unmarshal([]byte(`null`), &id3); err != nil || id3.Val != nil {
		t.Errorf("failed null id")
	}

	var errId SmartID
	if err := json.Unmarshal([]byte(`{"obj":1}`), &errId); err == nil {
		t.Errorf("expected error on obj id")
	}
}

func TestSmartID_MarshalJSON(t *testing.T) {
	id := SmartID{Val: "abc"}
	b, _ := id.MarshalJSON()
	if string(b) != `"abc"` {
		t.Error("marshal string failed")
	}

	id2 := SmartID{Val: nil}
	b2, _ := id2.MarshalJSON()
	if string(b2) != "null" {
		t.Error("marshal null failed")
	}
}

func TestRPCError_Error(t *testing.T) {
	err := &RPCError{Code: -32000, Message: "test"}
	if err.Error() != "RPC Error (-32000): test" {
		t.Error("error format incorrect")
	}
}

func TestInitializedNotification_MarshalJSON(t *testing.T) {
	n := InitializedNotification{}
	b, err := n.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` {
		t.Errorf("got %s", string(b))
	}
}
