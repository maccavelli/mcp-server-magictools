package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *OrchestratorHandler) registerResources(s *mcp.Server) {
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "mcp://magictools/raw/{id}",
		Name:        "Raw Tool Output",
		Description: "Fetches the full raw output of a previously summarized tool call. When a tool call via call_proxy returns more than 4,000 characters, it is summarized by the gateway to prevent context window saturation and protecting against OOM crashes. Use this resource URI (e.g., mcp://magictools/raw/...) to read the full, unmodified data when granular detail is required for your task. Mechanism: Retrieves the compressed tool output from the internal BadgerDB 'raw' store and decompresses it on the fly for your consumption.",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		id := strings.TrimPrefix(req.Params.URI, "mcp://magictools/raw/")
		data, err := h.Store.GetRawResource(id)
		if err != nil {
			return nil, fmt.Errorf("resource not found: %w", err)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{
				{
					URI:  req.Params.URI,
					Text: string(data),
				},
			},
		}, nil
	})
}
