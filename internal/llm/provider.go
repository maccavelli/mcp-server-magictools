package llm

import (
	"github.com/maccavelli/mcplib/llmprovider"
)

// Provider is a type alias re-exporting the shared library's Provider interface.
// This preserves backward compatibility for pool.go and pool_test.go.
type Provider = llmprovider.Provider

// ModelDiscoverer is a type alias re-exporting the shared library's ModelDiscoverer interface.
type ModelDiscoverer = llmprovider.ModelDiscoverer

// GenerateWithRetry delegates to the shared library implementation.
var GenerateWithRetry = llmprovider.GenerateWithRetry
