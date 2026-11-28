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
	return (&listToolsLayers{h: h, next: next}).handle
}

// listToolsLayers is the tools/list middleware, one method per layer. It is built once, when the middleware is
// wired, and holds only the handler and the next handler. It is never written after that, so concurrent calls
// share it safely, and it must stay that way: everything that belongs to one call (the request, the tools gathered
// so far) travels as an argument or a result, and nothing is kept between calls, because each call must read the
// session cache afresh.
type listToolsLayers struct {
	h    *OrchestratorHandler
	next mcp.MethodHandler
}

// handle is the method gate and the recover layer. Any method other than tools/list goes straight to next, with no
// recover. For tools/list, whatever a layer below panics with, the client gets an empty list and no error: the
// tools gathered before the panic are discarded with it.
func (l *listToolsLayers) handle(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
	if method != "tools/list" {
		return l.next(ctx, method, req)
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = recoveredListTools(r), nil
		}
	}()

	tools := l.base(ctx, method, req)
	tools = l.mergeSession(ctx, req, tools)
	return &mcp.ListToolsResult{Tools: tools}, nil
}

// recoveredListTools records a recovered panic and returns the empty list the client is given instead.
func recoveredListTools(r any) mcp.Result {
	telemetry.RecordPanic()
	slog.Error("orchestrator: PANIC RECOVERY during tools/list",
		"panic", r,
		"stack", string(debug.Stack()))
	return &mcp.ListToolsResult{Tools: []*mcp.Tool{}}
}

// base is the base layer: the high-fidelity internal tools the SDK registered with s.AddTool, as next returns them.
// When next fails, or returns a nil result, a result of another type, or a *mcp.ListToolsResult whose Tools slice is
// nil, the error is dropped and the static inventory is served instead. An empty, non-nil slice is a valid answer
// and is kept. A typed nil *mcp.ListToolsResult is not caught here: reading its Tools panics, and the recover in
// handle answers with the empty list, not the fallback. The base result's cursor and metadata are not carried over.
func (l *listToolsLayers) base(ctx context.Context, method string, req mcp.Request) []*mcp.Tool {
	slog.Log(ctx, util.LevelTrace, "orchestrator: invoking base tools/list handler")
	var tools []*mcp.Tool
	baseRes, err := l.next(ctx, method, req)
	if err == nil {
		if listRes, ok := baseRes.(*mcp.ListToolsResult); ok {
			tools = listRes.Tools
		}
	}
	if tools == nil {
		tools = l.staticInventory()
	}
	return tools
}

// staticInventory is the fallback of the base layer: one tool per non-nil inventory entry, with its name,
// description and input schema only. The schema is shared with the inventory, not copied and not sanitised
// (registration sanitises it, this path does not). The inventory is replaced by OnConfigReloaded under toolsMu, so
// it is read under the read lock, and only its slice header is copied under it: nothing is called while the lock
// is held, so it cannot take part in a lock cycle.
func (l *listToolsLayers) staticInventory() []*mcp.Tool {
	slog.Warn("orchestrator: base tools/list failed; falling back to static inventory")
	l.h.toolsMu.RLock()
	inventory := l.h.InternalTools
	l.h.toolsMu.RUnlock()
	tools := make([]*mcp.Tool, 0, len(inventory)+20)
	for _, it := range inventory {
		if it == nil {
			continue
		}
		tools = append(tools, &mcp.Tool{
			Name:        it.Name,
			Title:       it.Title,
			Description: it.Description,
			InputSchema: it.InputSchema,
			Annotations: it.Annotations,
		})
	}
	return tools
}

// mergeSession is the dynamic layer. Once the backplane is synced it appends, after the given tools and in the
// session cache's own order, the tools that session has discovered and cached that are not already listed by name.
// It extends and returns the slice it is given, so callers must use the result. The tools are the cached values
// themselves: they are not copied, re-sanitised or capped here (the bound is the cache's own capacity), and the
// cache is only read. Before the sync completes only the base layer is served.
func (l *listToolsLayers) mergeSession(ctx context.Context, req mcp.Request, tools []*mcp.Tool) []*mcp.Tool {
	if !l.h.Registry.IsSynced.Load() {
		slog.Log(ctx, util.LevelTrace, "orchestrator: backplane sync in progress; returning base layer only")
		return tools
	}
	lruInterface, ok := l.h.ActiveSessions.Load(listToolsSessionID(req))
	if !ok {
		return tools
	}
	lruTools := lruCacheFromAny(lruInterface).Values()
	seen := make(map[string]bool)
	for _, t := range tools {
		if t != nil {
			seen[t.Name] = true
		}
	}
	for _, t := range lruTools {
		if t != nil && !seen[t.Name] {
			tools = append(tools, t)
			seen[t.Name] = true
		}
	}
	return tools
}

// listToolsSessionID is the key of the session's tool cache: the request's session id as the SDK reports it, which
// is empty over the stdio and in-memory transports, or active-session when the request carries no session at all.
// An empty id is looked up as is, not mapped to active-session.
func listToolsSessionID(req mcp.Request) string {
	if req != nil && req.GetSession() != nil {
		return req.GetSession().ID()
	}
	return "active-session"
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
				// 0017: ValidateArguments coerces internally; the duplicate
				// CoerceAndStrip call that used to sit here is removed.
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
