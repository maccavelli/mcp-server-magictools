package handler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"
)

// SemanticSimilarityAudit is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) SemanticSimilarityAudit(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	slog.Info("semantic_similarity: starting full registry audit (Muzzle Protocol: os.Stderr)", "workers", 4)
	slog.Info("starting auditory analysis using 4 bounded threads", "component", "semantic_similarity")

	auditArgs := parseSimilarityAuditArgs(req.Params.Arguments)
	allTools, err := h.Store.SearchTools(ctx, "", "", "", 0, h.Config.ScoreFusionAlpha, db.DomainSystem, false)
	if err != nil {
		return nil, fmt.Errorf("failed to list all tools: %w", err)
	}
	if len(allTools) == 0 {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "No tools found in registry."}}}, nil
	}

	resultsChan := make(chan []similarityResult, len(allTools))
	var eg errgroup.Group
	eg.SetLimit(4)

	for _, t := range allTools {
		toolA := t
		if len(auditArgs.targetServers) > 0 && !auditArgs.targetServers[toolA.Server] {
			continue
		}
		eg.Go(func() error {
			if local := h.similarityFindMatches(ctx, toolA, auditArgs.targetServers); len(local) > 0 {
				resultsChan <- local
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		fmt.Fprintf(os.Stderr, "[semantic_similarity] Worker pool error: %v\n", err)
	}
	close(resultsChan)

	uniqueMatches := consolidateSimilarityMatches(resultsChan)
	markdown, _ := buildSimilarityAuditReport(uniqueMatches)
	if auditArgs.artifactPath != "" {
		dir := filepath.Dir(auditArgs.artifactPath)
		mkdirAllOrWarn(dir, 0o750)                                       //nolint:gosec // G301: user-specified artifact directory
		writeFileOrWarn(auditArgs.artifactPath, []byte(markdown), 0o600) //nolint:gosec // G306: user-local artifact
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("Artifact written to: %s", auditArgs.artifactPath)}}}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: markdown}}}, nil
}
