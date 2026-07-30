package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *OrchestratorHandler) handlePromptsList(ctx context.Context, next mcp.MethodHandler, method string, req mcp.Request) (mcp.Result, error) {
	baseRes, err := next(ctx, method, req)
	var finalPrompts []*mcp.Prompt
	if err == nil {
		if listRes, ok := baseRes.(*mcp.ListPromptsResult); ok {
			finalPrompts = listRes.Prompts
		}
	}
	if finalPrompts == nil {
		finalPrompts = []*mcp.Prompt{}
	}
	if h.PromptCache != nil {
		finalPrompts = append(finalPrompts, h.PromptCache.List()...)
	}
	return &mcp.ListPromptsResult{Prompts: finalPrompts}, nil
}

func (h *OrchestratorHandler) handlePromptsGet(ctx context.Context, next mcp.MethodHandler, method string, req mcp.Request) (mcp.Result, error) {
	callReq, ok := req.(*mcp.GetPromptRequest)
	if !ok || callReq.Params == nil {
		return next(ctx, method, req)
	}
	if !strings.Contains(callReq.Params.Name, ":") {
		return next(ctx, method, req)
	}
	parts := strings.SplitN(callReq.Params.Name, ":", 2)
	serverID, promptName := parts[0], parts[1]
	if serverID == serverMagictools {
		return next(ctx, method, req)
	}
	slog.Info("orchestrator: routing namespaced prompt call", "server", serverID, "prompt", promptName)
	sess, ok := h.Registry.GetServerSession(serverID)
	if !ok {
		slog.Warn("orchestrator: server not running for prompt proxy, applying graceful degradation", "server", serverID)
		return offlinePromptResult(serverID), nil
	}
	subReq := &mcp.GetPromptParams{
		Name:      promptName,
		Arguments: callReq.Params.Arguments,
	}
	proxyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := sess.GetPrompt(proxyCtx, subReq)
	if err != nil {
		slog.Warn("orchestrator: sub-server prompt timeout", "server", serverID, keyError, err)
		return timeoutPromptResult(serverID), nil
	}
	if res != nil {
		appendPromptMandate(proxyCtx, res, serverID, sess)
	}
	return h.MinifyPromptResponse(res), nil
}

func offlinePromptResult(serverID string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: "Proxy Error: Sub-server offline",
		Messages: []*mcp.PromptMessage{{
			Role: mcp.Role("user"),
			Content: &mcp.TextContent{
				Text: fmt.Sprintf("Error: Sub-server %s is offline or disconnected.", serverID),
			},
		}},
	}
}

func timeoutPromptResult(serverID string) *mcp.GetPromptResult {
	return &mcp.GetPromptResult{
		Description: "Proxy Error: Sub-server timed out",
		Messages: []*mcp.PromptMessage{{
			Role: mcp.Role("user"),
			Content: &mcp.TextContent{
				Text: fmt.Sprintf("Proxy Error: Sub-server %s timed out during prompt generation.", serverID),
			},
		}},
	}
}

func appendPromptMandate(ctx context.Context, res *mcp.GetPromptResult, serverID string, sess interface {
	ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
}) {
	var toolListStr string
	toolsRes, tErr := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if tErr == nil && toolsRes != nil {
		var urns []string
		for _, t := range toolsRes.Tools {
			urns = append(urns, fmt.Sprintf("- URN: %s:%s (Native Tool: %s)", serverID, t.Name, t.Name))
		}
		toolListStr = strings.Join(urns, "\n")
	}
	var mandateText string
	if toolListStr != "" {
		mandateText = fmt.Sprintf(`<orchestrator_mandate>
SYSTEM INSTRUCTION: You are operating through the magictools orchestrator proxy. 

For any tool listed in your available tools with a name containing a colon (e.g., 'server_name:tool_name'), you CAN call it directly.
For other downstream tools not in your available tools, you MUST execute them by using 'mcp_magictools_call_proxy' and supplying the correct URN from the list of resolved Proxy URNs below:
%s

Natively guessing proxy schemas is FORBIDDEN.
</orchestrator_mandate>`, toolListStr)
	} else {
		mandateText = fmt.Sprintf(`<orchestrator_mandate>
SYSTEM INSTRUCTION: You are operating through the magictools orchestrator proxy. 

For any tool listed in your available tools with a name containing a colon (e.g., 'server_name:tool_name'), you CAN call it directly.
For other downstream tools not in your available tools, you MUST discover their URN/schema via 'mcp_magictools_align_tools' (using server_name: "%s") and execute them EXCLUSIVELY via 'mcp_magictools_call_proxy'. Natively guessing proxy schemas is FORBIDDEN.
</orchestrator_mandate>`, serverID)
	}
	res.Messages = append(res.Messages, &mcp.PromptMessage{
		Role: mcp.Role("user"),
		Content: &mcp.TextContent{
			Text: mandateText,
		},
	})
}
