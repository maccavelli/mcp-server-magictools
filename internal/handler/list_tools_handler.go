package handler

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListToolsMiddleware intercepts 'tools/list' to provide robust recovery and aggregation.
func (h *OrchestratorHandler) ListToolsMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
		if method != "tools/list" {
			return next(ctx, method, req)
		}

		// 🛡️ RECOVERY BLOCK: Prevent orchestrator crashes during tool listing.
		defer func() {
			if r := recover(); r != nil {
				telemetry.RecordPanic()
				slog.Error("orchestrator: PANIC RECOVERY during tools/list",
					"panic", r,
					"stack", string(debug.Stack()))

				if result == nil {
					result = &mcp.ListToolsResult{
						Tools: []*mcp.Tool{},
					}
				}
				err = nil
			}
		}()

		// 1. BASE LAYER: Start with high-fidelity internal tools from the SDK.
		// These were registered via s.AddTool earlier and should be returned by the base handler.
		slog.Log(ctx, util.LevelTrace, "orchestrator: invoking base tools/list handler")
		baseRes, err := next(ctx, method, req)

		var finalTools []*mcp.Tool
		if err == nil {
			if listRes, ok := baseRes.(*mcp.ListToolsResult); ok {
				finalTools = listRes.Tools
			}
		}

		// 🛡️ RECOVERY: If the base handler failed or returned nil, fallback to internal inventory directly.
		if finalTools == nil {
			slog.Warn("orchestrator: base tools/list failed; falling back to static inventory")
			finalTools = make([]*mcp.Tool, 0, len(h.InternalTools)+20)
			for _, it := range h.InternalTools {
				if it == nil {
					continue
				}
				finalTools = append(finalTools, &mcp.Tool{
					Name:        it.Name,
					Description: it.Description,
					InputSchema: it.InputSchema,
				})
			}
		}

		// 2. DYNAMIC LAYER: Append aggregate tools from the ActiveSessions.
		// 🛡️ LRU BOUNDING: Instead of dumping all tools from the Backplane, we only
		// serve the recently discovered tools (max 20) to strictly cap the context window.
		if !h.Registry.IsSynced.Load() {
			slog.Log(ctx, util.LevelTrace, "orchestrator: backplane sync in progress; returning base layer only")
		} else {
			sessionID := "active-session"
			if req != nil && req.GetSession() != nil {
				sessionID = req.GetSession().ID()
			}
			if lruInterface, ok := h.ActiveSessions.Load(sessionID); ok {
				lruCache := lruCacheFromAny(lruInterface)
				lruTools := lruCache.Values()
				seen := make(map[string]bool)
				for _, t := range finalTools {
					if t != nil {
						seen[t.Name] = true
					}
				}
				for _, t := range lruTools {
					if t != nil && !seen[t.Name] {
						finalTools = append(finalTools, t)
						seen[t.Name] = true
					}
				}
			}
		}

		return &mcp.ListToolsResult{
			Tools: finalTools,
		}, nil
	}
}

// CallToolMiddleware intercepts 'tools/call' to correctly route namespaced tool calls.
func (h *OrchestratorHandler) CallToolMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, req)
		}

		callReq, ok := req.(*mcp.ServerRequest[*mcp.CallToolParams])
		if !ok || callReq.Params == nil {
			return next(ctx, method, req)
		}

		// 🛡️ NAMESPACED ROUTING: If the tool name contains a colon, it's a namespaced sub-server tool.
		if strings.Contains(callReq.Params.Name, ":") {
			parts := strings.SplitN(callReq.Params.Name, ":", 2)
			serverID, toolName := parts[0], parts[1]

			slog.Info("orchestrator: routing namespaced tool call", "server", serverID, "tool", toolName)

			// 🛡️ ARGUMENT NORMALIZATION: Use RawMessage as-is or transform if needed.
			var args map[string]any
			if callReq.Params.Arguments != nil {
				// The SDK already provides map[string]any if it parsed correctly.
				// If not, we attempt to treat it as such.
				if am, ok := callReq.Params.Arguments.(map[string]any); ok {
					args = am
				} else {
					data := marshalJSONOrEmpty(callReq.Params.Arguments) //nolint:errcheck // safe: SDK-provided value
					unmarshalOrWarn(data, &args)                         //nolint:errcheck // best-effort type coercion fallback
				}
			}

			// 🛡️ LAZY ACTIVATION: Ensure sub-server is running and Ready.
			if srv, ok := h.Registry.GetServer(serverID); !ok || srv.Status != client.StatusReady || srv.Session == nil {
				var targetConfig *config.ServerConfig
				for _, sc := range h.Config.GetManagedServers() {
					if sc.Name == serverID {
						targetConfig = &sc
						break
					}
				}

				if targetConfig == nil {
					return nil, fmt.Errorf("server %q not found in managed config; cannot route to %q", serverID, toolName)
				}

				slog.Info("orchestrator: lazy-activating server for namespaced call", "server", serverID)
				if err := h.Registry.Connect(ctx, targetConfig.Name, targetConfig.Command, targetConfig.Args, targetConfig.Env, targetConfig.Hash()); err != nil {
					return nil, fmt.Errorf("failed to lazy-activate server %s: %w", serverID, err)
				}
			}

			// 🛡️ UNIFIED VALIDATION GATEWAY: Route through ProxyService to ensure namespaced calls
			// receive identical validation, Tier 1 auto-coercion, and argument repair as call_proxy.
			// Without this, direct namespaced calls bypass the entire self-healing pipeline.
			ps := NewProxyService(h)
			urn := serverID + ":" + toolName
			_, _, _, toolRecord, resolveErr := ps.ResolveURN(ctx, urn)
			if resolveErr != nil {
				return nil, fmt.Errorf("namespaced call URN resolution failed for %s: %w", urn, resolveErr)
			}

			if h.Config.ValidateProxyCalls && toolRecord != nil {
				ps.CoerceAndStrip(toolRecord, args)
				if valErr := ps.ValidateArguments(ctx, urn, toolRecord, args); valErr != nil {
					slog.Warn("orchestrator: namespaced call pre-flight validation failed", keyURN, urn, keyError, valErr)
					return &mcp.CallToolResult{
						IsError: true,
						Content: []mcp.Content{&mcp.TextContent{Text: valErr.Error()}},
					}, nil
				}
			}

			// Route the validated call to the appropriate sub-server on the Backplane.
			// 🛡️ UNIFIED TIMEOUT: Use shared resolution instead of hardcoded 15s.
			timeout := ResolveTimeout(toolRecord)
			res, callErr := h.Registry.CallProxy(ctx, serverID, toolName, args, timeout)
			if callErr != nil {
				return nil, callErr
			}

			// 🛡️ UNIFIED RESPONSE PIPELINE: Apply the same post-processing that
			// call_proxy uses, ensuring namespaced calls get minification, size caps,
			// and telemetry. Without this, raw multi-MB responses flow to the IDE.
			return ps.PostProcessResponse(ctx, res, serverID, toolName, urn, toolRecord, args)
		}

		// Fallback to standard SDK routing for meta-tools (align_tools, etc.)
		return next(ctx, method, req)
	}
}
