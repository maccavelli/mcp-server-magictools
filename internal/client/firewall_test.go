package client

import (
	"testing"
)

func TestFirewallValidateValidRequest(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","method":"tools/list","id":1}`)
	if !fw.validate(frame) {
		t.Error("valid request rejected")
	}
}

func TestFirewallValidateValidResponse(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{"tools":[]},"id":1}`)
	if !fw.validate(frame) {
		t.Error("valid response rejected")
	}
}

func TestFirewallValidateValidErrorResponse(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"Invalid Request"},"id":"abc"}`)
	if !fw.validate(frame) {
		t.Error("valid error response rejected")
	}
}

func TestFirewallValidateValidNotification(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if !fw.validate(frame) {
		t.Error("valid notification rejected")
	}
}

func TestFirewallValidateNullID(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{},"id":null}`)
	if !fw.validate(frame) {
		t.Error("valid null-id response rejected")
	}
}

func TestFirewallRejectMissingJSONRPC(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"method":"tools/list","id":1}`)
	if fw.validate(frame) {
		t.Error("frame without jsonrpc field should be rejected")
	}
}

func TestFirewallRejectWrongVersion(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"1.0","method":"tools/list","id":1}`)
	if fw.validate(frame) {
		t.Error("frame with wrong jsonrpc version should be rejected")
	}
}

func TestFirewallRejectTextBleed(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	// Simulates a log message that happens to be valid JSON
	frame := []byte(`{"level":"error","msg":"something failed","time":"2026-01-01T00:00:00Z"}`)
	if fw.validate(frame) {
		t.Error("log message text bleed should be rejected")
	}
}

func TestFirewallRejectMutualExclusivityViolation(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	// Both method and result — invalid per JSON-RPC 2.0
	frame := []byte(`{"jsonrpc":"2.0","method":"tools/list","result":{},"id":1}`)
	if fw.validate(frame) {
		t.Error("frame with both method and result should be rejected")
	}
}

func TestFirewallRejectResponseWithoutID(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{}}`)
	if fw.validate(frame) {
		t.Error("response without id should be rejected")
	}
}

func TestFirewallRejectEmptyObject(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0"}`)
	if fw.validate(frame) {
		t.Error("object with only jsonrpc field should be rejected")
	}
}

func TestFirewallRejectMalformedJSON(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","method":`)
	if fw.validate(frame) {
		t.Error("malformed JSON should be rejected")
	}
}

func TestFirewallRejectNonObject(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`[{"jsonrpc":"2.0","method":"test"}]`)
	if fw.validate(frame) {
		t.Error("array should be rejected")
	}
}

func TestFirewallRejectInvalidIDType(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	// ID is an object — invalid per JSON-RPC 2.0
	frame := []byte(`{"jsonrpc":"2.0","result":{},"id":{"complex":true}}`)
	if fw.validate(frame) {
		t.Error("object-typed id should be rejected")
	}
}

func TestFirewallRejectArrayID(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{},"id":[1,2]}`)
	if fw.validate(frame) {
		t.Error("array-typed id should be rejected")
	}
}

func TestFirewallAcceptsStringID(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{},"id":"req-42"}`)
	if !fw.validate(frame) {
		t.Error("valid string id should be accepted")
	}
}

func TestFirewallAcceptsNegativeNumericID(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	frame := []byte(`{"jsonrpc":"2.0","result":{},"id":-1}`)
	if !fw.validate(frame) {
		t.Error("valid negative numeric id should be accepted")
	}
}

func TestFirewallSnapshot(t *testing.T) {
	fw := &jsonrpcFirewall{serverName: "test-server"}
	// Validate a good and bad frame to populate counters
	fw.validate([]byte(`{"jsonrpc":"2.0","method":"ping","id":1}`))
	fw.ValidFrames.Add(1)
	fw.validate([]byte(`{"bad":"data"}`))
	fw.DroppedFrames.Add(1)

	snap := fw.Snapshot()
	if snap["valid_frames"].(int64) != 1 {
		t.Errorf("expected 1 valid frame, got %v", snap["valid_frames"])
	}
	if snap["dropped_frames"].(int64) != 1 {
		t.Errorf("expected 1 dropped frame, got %v", snap["dropped_frames"])
	}
}
