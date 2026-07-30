package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

func TestProcessorSanitize(t *testing.T) {
	conn := &DecoderConnection{
		stop:            make(chan struct{}),
		resChan:         make(chan readResult, 1),
		pendingRequests: &sync.Map{},
	}
	p := newMessageProcessor(conn)

	// Test missing fields
	raw := json.RawMessage(`{"empty":true}`)
	res := p.sanitizeInitializeResult(raw)
	var m map[string]any
	json.Unmarshal(res, &m)
	if m["protocolVersion"] != "2024-11-05" {
		t.Error("expected missing fields to be populated")
	}

	// Test invalid JSON string
	badRaw := json.RawMessage(`bad json`)
	badRes := p.sanitizeInitializeResult(badRaw)
	if string(badRes) != "bad json" {
		t.Error("expected untouched on bad json")
	}
}

func TestProcessorHandleError(t *testing.T) {
	conn := &DecoderConnection{
		stop:            make(chan struct{}),
		resChan:         make(chan readResult, 1),
		pendingRequests: &sync.Map{},
	}
	p := newMessageProcessor(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*10)
	defer cancel()

	ok := p.handleError(ctx, fmt.Errorf("Unknown message type: blah"), nil)
	if !ok {
		t.Error("expected true for Unknown message type")
	}

	ok = p.handleError(ctx, fmt.Errorf("fatal connection error"), nil)
	if ok {
		t.Error("expected false for fatal error")
	}
}

func TestProcessorProcess(t *testing.T) {
	conn := &DecoderConnection{
		stop:            make(chan struct{}),
		resChan:         make(chan readResult, 1),
		pendingRequests: &sync.Map{},
	}
	p := newMessageProcessor(conn)

	// Test bad json
	_, err := p.process(json.RawMessage(`bad`))
	if err == nil {
		t.Error("expected error")
	}

	// Test standard JSONRPC notification
	raw := json.RawMessage(`{"jsonrpc":"2.0","method":"foo"}`)
	msg, err := p.process(raw)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if msg == nil {
		t.Error("expected message")
	}

	// Test initialize response
	rawResp := json.RawMessage(`{"jsonrpc":"2.0","id":"my-id","result":{"a":1}}`)

	msg, err = p.process(rawResp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	_, ok := msg.(*jsonrpc.Response)
	if !ok {
		t.Fatal("expected response")
	}
}
