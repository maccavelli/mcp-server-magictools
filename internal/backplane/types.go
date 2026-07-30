// Package backplane provides functionality for the backplane subsystem.
package backplane

import (
	"encoding/json"
	"fmt"
)

// SmartID handles both numeric and string IDs in JSON-RPC.
type SmartID struct {
	Val any
}

// UnmarshalJSON handles both numeric (1) and string ("abc") IDs.
func (s *SmartID) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		s.Val = nil
		return nil
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v.(type) {
	case string, float64, int, int64:
		s.Val = v
	default:
		return fmt.Errorf("invalid JSON-RPC ID type: %T", v)
	}
	return nil
}

// MarshalJSON ensures it serializes back to its original JSON type.
func (s SmartID) MarshalJSON() ([]byte, error) {
	if s.Val == nil {
		return []byte("null"), nil
	}
	return json.Marshal(s.Val)
}

// RPCError is a concrete struct for successful JSON unmarshaling of errors.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error is undocumented but satisfies standard structural requirements.
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC Error (%d): %s", e.Code, e.Message)
}

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty,omitzero"` // Pointer for absolute omission in notifications
	Method  string          `json:"method"`
	Params  any             `json:"params,omitempty,omitzero"`
}

// InitializeParams defines the parameters for the MCP initialize request.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// ClientCapabilities defines the capabilities supported by the client.
type ClientCapabilities struct {
	Roots    *RootsCapability    `json:"roots,omitempty,omitzero"`
	Sampling *SamplingCapability `json:"sampling,omitempty,omitzero"`
}

// RootsCapability defines roots-related capabilities.
type RootsCapability struct {
	ListChanged bool `json:"listChanged"`
}

// SamplingCapability defines sampling-related capabilities.
type SamplingCapability struct{}

// ClientInfo defines information about the client implementation.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Notification represents a generic JSON-RPC 2.0 notification.
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty,omitzero"`
}

// InitializedNotification is a dedicated struct for MCP initialize notifications.
// Do not reuse the standard Request struct, as notifications must never contain an id field.
type InitializedNotification struct {
	JSONRPC string   `json:"jsonrpc"`
	Method  string   `json:"method"`
	Params  struct{} `json:"params"` // Must be an empty object, not null
}

// MarshalJSON ensures the JSONRPC and Method fields are hardcoded to "2.0" and "notifications/initialized".
func (n InitializedNotification) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		JSONRPC string   `json:"jsonrpc"`
		Method  string   `json:"method"`
		Params  struct{} `json:"params"`
	}{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
		Params:  struct{}{},
	})
}

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty,omitzero"`
	Error   *RPCError       `json:"error,omitempty,omitzero"`
}

// ListToolsRequest represents a tools/list request.
type ListToolsRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
}

// ListToolsResult represents the result of a tools/list request.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// Tool represents a single tool definition in MCP.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}
