// Package handler implements MCP tool handlers for the magictools orchestrator.
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/singleflight"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/prompt_registry"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcplib"

	lru "github.com/hashicorp/golang-lru/v2"
)

// InternalTool represents an internal tool with metadata like category
type InternalTool struct {
	Name        string               `json:"name"`
	Title       string               `json:"title,omitempty"`
	Description string               `json:"description"`
	Category    string               `json:"category,omitempty,omitzero"`
	InputSchema any                  `json:"inputSchema"`
	Annotations *mcp.ToolAnnotations `json:"annotations,omitempty"`
}

// OrchestratorHandler handles magictools logic
type OrchestratorHandler struct {
	Store            *db.Store
	Registry         *client.WarmRegistry
	Config           *config.Config
	Telemetry        *telemetry.Tracker
	Responses        *db.ResponseCache
	RecallClient     *mcplib.RecallClient                 // 🛡️ RECALL: Direct HTTP client for PM pipeline recall queries
	PipelineEnabled  *atomic.Bool                         // 🛡️ PIPELINE: Gate-check for active code generation pipeline
	HydratorSignal   chan struct{}                        // 🛡️ HYDRATOR GATE: Non-blocking trigger signal for the daemon
	InternalTools    []*InternalTool                      // 🛡️ STATIC INVENTORY: Hardcoded internal tools list with categories
	LogLevel         *slog.LevelVar                       // 🛡️ DYNAMIC LEVEL: Shared with all slog handlers for live reload
	loopbackHandlers map[string]mcp.ToolHandler           // 🛡️ LOOPBACK: Dispatch map for native tool self-routing
	toolsMu          sync.RWMutex                         // Protects InternalTools from concurrent access
	schemaCache      *lru.Cache[string, any]              // 🛡️ PERF: SchemaHash -> *jsonschema.Schema (compiled, reusable)
	AlignCache       *lru.Cache[string, *AlignCacheEntry] // 🛡️ PERF: LRU cache for align_tools intents
	alignGen         atomic.Uint64                        // 🛡️ ALIGN-2: bumped on tool sync/reload/purge; folded into the align cache key so a re-sync can't serve a stale schema
	ActiveSessions   sync.Map                             // 🛡️ SYSTEM PROMPT: Maps session_id -> *util.S3FIFOCache[string, *mcp.Tool]
	PromptCache      *prompt_registry.Cache               // 🛡️ PROMPT REGISTRY: Two-tier dynamic prompt cache
	Server           *mcp.Server                          // 🛡️ NOTIFICATIONS: Used to fire list_changed to IDE
	LLMPool          *llm.Pool                            // 🛡️ LLM BACKPLANE: Shared pool for metrics surfacing (nil if disabled)
	LazyBootGroup    singleflight.Group                   // 🛡️ SINGLEFLIGHT: Shared across all ProxyService instances for lazy-boot coalescing

	// Pipeline continuation (0013): the session store, this instance's nonce,
	// and test seams. pipelineSessions is bound lazily under pipelineSessionsOnce.
	pipelineSessions        *db.PipelineSessionStore
	pipelineSessionsOnce    sync.Once
	pipelineInstanceNonce   string
	pipelineTestCountStep   func()        // test seam: counts stage executions
	pipelineTestInjectStage *PipelineStep // test seam: injects a stage into a dry-run plan
}

// SessionStats has been removed in favor of telemetry.Tracker

// NewHandler creates a handler
func NewHandler(store *db.Store, registry *client.WarmRegistry, cfg *config.Config) *OrchestratorHandler {
	alignCache := newLRUOrWarn[string, *AlignCacheEntry](defaultAlignCacheSize)
	schemaCache := newLRUOrWarn[string, any](defaultAlignCacheSize)
	h := &OrchestratorHandler{
		Store:            store,
		Registry:         registry,
		Config:           cfg,
		Telemetry:        telemetry.GlobalTracker,
		Responses:        db.NewResponseCache(cfg.LRULimit),
		InternalTools:    make([]*InternalTool, 0),
		loopbackHandlers: make(map[string]mcp.ToolHandler),
		AlignCache:       alignCache,
		schemaCache:      schemaCache,
		PromptCache:      prompt_registry.NewCache(store.DB),
	}

	// 🛡️ PROMPT REGISTRY: Hydrate Tier 1 from Tier 2 BadgerDB
	h.PromptCache.HydrateFromDB()

	// 🛡️ PROMPT REGISTRY: Handle dynamic prompt list updates from sub-servers
	h.Registry.OnPromptListChanged = func(serverName string) {
		session, ok := h.Registry.GetServerSession(serverName)
		if ok {
			h.PromptCache.Refresh(context.Background(), serverName, session)
		}
	}

	// 🛡️ NATIVE REGISTRY LOADING: Pre-populate internal tools from the static inventory.
	// This uses the High-Fidelity schemas defined in inventory.go.
	if err := json.Unmarshal(InternalToolsInventoryJSON, &h.InternalTools); err != nil {
		slog.Error("orchestrator: FAILED to unmarshal static tools inventory", keyError, err)
	}

	h.syncSearchGates()

	return h
}

func (h *OrchestratorHandler) syncSearchGates() {
	if h.Store == nil || h.Config == nil {
		return
	}
	h.Store.SearchGates = db.SearchGatesFromConfig(
		h.Config.StrictGates,
		h.Config.VectorMinCosine,
		h.Config.BM25MinNormalized,
		h.Config.DisableSearchFallback,
	)
	h.Store.Fusion = db.FusionFromConfig(
		h.Config.CorroborationBonus,
		h.Config.ReliabilityBoost,
		h.Config.UsageBoost,
		h.Config.NativeBoost,
	)
	if h.AlignCache != nil {
		h.AlignCache.Purge()
	}
}

// toSchemaMap converts a schema (any) into a map[string]interface{}.
func (h *OrchestratorHandler) toSchemaMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	if res, ok := schema.(map[string]any); ok {
		return res
	}
	data := marshalJSONOrEmpty(schema) //nolint:errcheck // safe: schema is always marshallable
	var out map[string]any
	unmarshalOrWarn(data, &out) //nolint:errcheck // safe: roundtrip from valid schema
	return out
}

// OnConfigReloaded is called when the configuration file has been re-read.
func (h *OrchestratorHandler) OnConfigReloaded(cfg *config.Config) {
	slog.Info("orchestrator: configuration reloaded, refreshing internal tools")

	var newTools []*InternalTool
	if err := json.Unmarshal(InternalToolsInventoryJSON, &newTools); err != nil {
		slog.Error("orchestrator: FAILED to refresh static tools inventory", keyError, err)
		return
	}

	h.toolsMu.Lock()
	h.InternalTools = newTools
	h.toolsMu.Unlock()
	slog.Info("orchestrator: internal tools refreshed", "count", len(newTools))

	// 🛡️ DYNAMIC LOG LEVEL: Apply the new log level from the reloaded config at runtime.
	if h.LogLevel != nil && cfg.LogLevel != "" {
		newLevel := logging.ParseLogLevel(cfg.LogLevel)
		oldLevel := h.LogLevel.Level()
		if newLevel != oldLevel {
			h.LogLevel.Set(newLevel)
			slog.Info("orchestrator: log level changed", "old", oldLevel.String(), "new", newLevel.String())
		}
	}

	// The tools already registered with the SDK server are not re-registered: the SDK has no
	// easy way to replace them. The refreshed inventory reaches clients through the static
	// inventory fallback of ListToolsMiddleware and through align_tools, which both read
	// h.InternalTools; the base tools/list layer keeps serving what was registered at startup.

	h.syncSearchGates()
}

// OnMCPLogLevelChanged routes dynamic config-driven log-level patches by forcing an ecosystem restart.
func (h *OrchestratorHandler) OnMCPLogLevelChanged(oldLevel, newLevel string) {
	slog.Warn("sub-server mcp log level mutated; forcing graceful recycle", "component", "watchdog", "old", oldLevel, "new", newLevel)
	// Passing an empty request natively forces the orchestrator to perform an ecosystem reload
	reloadServersOrWarn(h)
}

// Close gracefully terminates all orchestrated sub-servers and internal systems.d by all slog handlers.
func (h *OrchestratorHandler) SetLogLevel(lv *slog.LevelVar) {
	h.LogLevel = lv
}

// Register attaches tools and resources to the server. It must complete before the server serves any request:
// registration writes into the inventory's schema maps (see addTool), and the readers of those maps are request handlers.
func (h *OrchestratorHandler) Register(s *mcp.Server) {
	h.Server = s
	slog.Log(context.Background(), util.LevelTrace, "orchestrator: registering handlers and tools")

	// 🛡️ BASTION SAFETY: Add custom middleware for handlers with robust recovery,
	// tool namespacing, and dynamic aggregation.
	s.AddReceivingMiddleware(h.ListToolsMiddleware)
	s.AddReceivingMiddleware(h.CallToolMiddleware)
	s.AddReceivingMiddleware(h.PromptsMiddleware)

	h.registerSyncTools(s)
	h.registerProxyTools(s)
	h.registerMaintenanceTools(s)
	h.registerDiagnosticTools(s)
	h.registerResources(s)

	h.toolsMu.RLock()
	registered := len(h.InternalTools)
	h.toolsMu.RUnlock()
	slog.Info("orchestrator: registration complete", "internal_tools", registered)
}

// addTool wraps s.AddTool with schema sanitization to prevent SDK validation panics.
//
// It reads the static inventory under toolsMu, because the config watcher replaces it (OnConfigReloaded): the slice
// header is copied under the read lock, the copy is iterated, and the lock is released before the loop and before
// s.AddTool, so no lock is held across the SDK call.
//
// It also WRITES. util.SanitizeToolSchema edits the inventory element's schema map in place (it adds a missing type
// or properties, and re-assigns every nested property), and the SDK is handed that same map. That is safe only
// because registration completes before the server serves anything: every reader of those maps runs only while a
// client request is being handled. That includes the SDK itself: Server.AddTool keeps the tool, and with it the
// same inventory map (its own remarshal only checks a copy), and the SDK's tools/list marshals that map on every
// call. The other readers are the tools/list fallback and align_tools. The config watcher's reload builds a
// fresh slice from the inventory JSON and never touches the old elements; and nothing started before Register reads
// them. A reader of the maps concurrent with registration would race with it. Sanitising a copy instead would remove
// the write, and would change what the tools/list fallback emits; that is left to the follow-up record that owns the
// fallback's schemas (docs/decisions/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md, N2 and N3).
func (h *OrchestratorHandler) addTool(s *mcp.Server, t *mcp.Tool, handler mcp.ToolHandler) {
	// 🛡️ METADATA CONSOLIDATION: Prefer metadata from the static inventory if it exists.
	// This ensures total adherence to the SDK-friendly schemas defined in inventory.go.
	target := t
	h.toolsMu.RLock()
	inventory := h.InternalTools
	h.toolsMu.RUnlock()
	for _, it := range inventory {
		if it.Name == t.Name {
			slog.Log(context.Background(), util.LevelTrace, "orchestrator: overriding registration metadata from static inventory", "tool", t.Name)
			// Map InternalTool to mcp.Tool
			target = &mcp.Tool{
				Name:        it.Name,
				Title:       it.Title,
				Description: it.Description,
				InputSchema: it.InputSchema,
				Annotations: it.Annotations,
			}
			break
		}
	}

	// 🛡️ BASTION SAFETY: Ensure tool schema is valid before passing to the SDK.
	sanitized := util.SanitizeToolSchema(target)

	// 🛡️ LOOPBACK: Store handler for direct in-process dispatch when proxy resolves magictools:* URNs.
	h.loopbackHandlers[sanitized.Name] = handler

	slog.Log(context.Background(), util.LevelTrace, "orchestrator: registering internal tool", "name", sanitized.Name)
	s.AddTool(sanitized, handler)
}
