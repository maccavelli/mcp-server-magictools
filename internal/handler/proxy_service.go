package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// ProxyService handles URN resolution, lazy activation, proxy execution,
// and response minification. Extracted from registerProxyGateway to reduce
// cognitive complexity.
type ProxyService struct {
	Handler *OrchestratorHandler
}

// NewProxyService creates a ProxyService backed by the given handler.
func NewProxyService(h *OrchestratorHandler) *ProxyService {
	return &ProxyService{Handler: h}
}

// hydrateRecord resolves schema:{hash} into InputSchema and ZeroValues for proxy paths.
func (ps *ProxyService) hydrateRecord(record *db.ToolRecord) {
	if record == nil || ps.Handler == nil || ps.Handler.Store == nil {
		return
	}
	ps.Handler.Store.HydrateToolSchema(record)
}

// AutoCoerceArguments rapidly injects missing properties with zero-values or defaults natively
// utilizing the pre-computed ZeroValues profile directly from the cached ToolRecord.
// This executes in O(1) latency cleanly offloading formatting burdens from the LLM agent.
func (ps *ProxyService) AutoCoerceArguments(record *db.ToolRecord, args map[string]any) {
	if record == nil {
		return
	}
	ps.injectZeroValues(record, args)
	ps.coerceSchemaTypes(record, args)
}

// ValidateArguments asserts schema constraints on incoming payloads natively.
// When record is non-nil, it skips the redundant GetTool DB lookup.
// Compiled schemas are cached by SchemaHash for reuse across calls seamlessly.
func (ps *ProxyService) ValidateArguments(ctx context.Context, urn string, record *db.ToolRecord, args map[string]any) error {
	if record == nil {
		var err error
		record, err = ps.Handler.Store.GetTool(urn)
		if err != nil {
			return err
		}
	}
	ps.hydrateRecord(record)
	hash := record.SchemaHash
	if hash == "" {
		return nil
	}

	ps.CoerceAndStrip(record, args)

	// 🛡️ PERF: Check compiled schema cache first natively
	if cached, ok := ps.Handler.schemaCache.Get(hash); ok {
		sch := schemaFromCacheOrWarn(cached)
		if err := sch.Validate(args); err != nil {
			return ps.formatStructuredCorrectionError(urn, record, args, err)
		}
		return nil
	}

	// Cache miss: fetch, compile, and store organically
	schema, err := ps.Handler.Store.GetSchema(hash)
	if err != nil || len(schema) == 0 {
		if err == nil {
			err = fmt.Errorf("empty schema")
		}
		return ps.failClosedForMutator(record, urn, "schema-fetch", err)
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return ps.failClosedForMutator(record, urn, "schema-marshal", err)
	}

	c := jsonschema.NewCompiler()
	resourceName := fmt.Sprintf("schema/%s.json", hash)
	if err := c.AddResource(resourceName, bytes.NewReader(schemaBytes)); err != nil {
		return ps.failClosedForMutator(record, urn, "schema-add-resource", err)
	}
	sch, err := c.Compile(resourceName)
	if err != nil {
		return ps.failClosedForMutator(record, urn, "schema-compile", err)
	}
	ps.Handler.schemaCache.Add(hash, sch)

	if err := sch.Validate(args); err != nil {
		return ps.formatStructuredCorrectionError(urn, record, args, err)
	}
	return nil
}

// failClosedForMutator implements PROXY-M2: when the pre-flight schema firewall
// can't run because of an INFRASTRUCTURE error (schema fetch/marshal/compile),
// a mutator must NOT be allowed to proceed unvalidated — surface the error.
// Read-only tools log and proceed (the firewall is best-effort for them).
func (ps *ProxyService) failClosedForMutator(record *db.ToolRecord, urn, stage string, cause error) error {
	if record != nil && record.Role == roleMutator {
		return fmt.Errorf("pre-flight validation unavailable for mutator %q (%s): %w", urn, stage, cause)
	}
	slog.Warn("gateway: schema validation skipped (infrastructure error)", keyURN, urn, "stage", stage, keyError, cause)
	return nil
}

// reservedEnvelopeKeys are MCP params-level metadata keys (notably keyMeta) that are
// never valid tool arguments. They must be stripped from the argument payload before
// validation, caching, or sub-server dispatch — sub-servers with additionalProperties:false
// reject unknown keys. This set is the single source of truth for reserved-key handling,
// referenced by both stripExtraProperties (validation path) and stripReservedKeys
// (dispatch funnel). The final wire-boundary backstop lives in client.WarmRegistry.CallProxy.
var reservedEnvelopeKeys = map[string]bool{keyMeta: true}

// stripReservedKeys removes reserved envelope metadata (reservedEnvelopeKeys) from a
// parsed argument map, returning the proxy correlation id if a legacy caller embedded
// one under _meta. Safe to call on a nil map. This is the canonical reserved-key
// authority for the dispatch funnel.
func stripReservedKeys(args map[string]any) (corrID string) {
	if args == nil {
		return ""
	}
	if meta, ok := args[keyMeta].(map[string]any); ok {
		if id, ok := meta["proxy_correlation_id"].(string); ok {
			corrID = id
		}
	}
	for key := range reservedEnvelopeKeys {
		delete(args, key)
	}
	return corrID
}

func stripExtraProperties(schema map[string]any, args map[string]any) {
	if schema == nil || args == nil {
		return
	}
	props, ok := schema[keyProperties].(map[string]any)
	if !ok {
		return
	}
	for key, val := range args {
		if reservedEnvelopeKeys[key] {
			// Reserved envelope metadata, not a hallucinated field — strip quietly.
			delete(args, key)
			continue
		}
		propSchemaRaw, exists := props[key]
		if !exists {
			slog.Info("gateway: Tier1 stripped hallucinated field", "field", key)
			telemetry.ProxyOptimization.ExtraFieldsStripped.Add(1)
			delete(args, key)
			continue
		}
		propSchema, ok := propSchemaRaw.(map[string]any)
		if !ok {
			continue
		}
		typ := stringFrom(propSchema[keyType])
		if typ == schemaTypeObject || propSchema[keyProperties] != nil {
			if nestedArgs, ok := val.(map[string]any); ok {
				stripExtraProperties(propSchema, nestedArgs)
			}
		}
	}
}

// CoerceAndStrip applies Tier 1 safe coercion: ZeroValues injection, type coercion,
// enum snapping (via AutoCoerceArguments), followed by stripping hallucinated extra
// fields not present in schema.properties. This ensures only schema-defined fields
// remain, preventing additionalProperties:false validation failures from extra keys.
func (ps *ProxyService) CoerceAndStrip(record *db.ToolRecord, args map[string]any) {
	if record == nil || args == nil {
		return
	}
	ps.hydrateRecord(record)
	ps.AutoCoerceArguments(record, args)

	// Strip extra hallucinated fields not defined in schema.properties (recursively)
	if record.InputSchema != nil {
		stripExtraProperties(record.InputSchema, args)
	}
}

// formatStructuredCorrectionError returns a Tier 2 structured correction template
// showing the agent exactly which required fields are missing and providing a
// ready-to-use call_proxy template. This eliminates guesswork on retry.
func (ps *ProxyService) formatStructuredCorrectionError(urn string, record *db.ToolRecord, args map[string]any, baseErr error) error {
	var errMsg string
	if baseErr != nil {
		errMsg = baseErr.Error()
	}
	ps.hydrateRecord(record)
	correction := map[string]any{
		keyError:           errorMissingRequiredFields,
		keyURN:             urn,
		"your_arguments":   args,
		"validation_error": errMsg,
	}

	if record != nil && record.InputSchema != nil {
		// Extract required set from schema
		var requiredFields []string
		if req, ok := record.InputSchema[keyRequired].([]any); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					requiredFields = append(requiredFields, s)
				}
			}
		}

		var missing []string
		for _, key := range requiredFields {
			if _, exists := args[key]; !exists {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			correction["missing_required"] = missing
		}

		// Build a call template with optional fields pre-filled
		tmplArgs := make(map[string]any)
		requiredSet := make(map[string]bool)
		for _, k := range requiredFields {
			requiredSet[k] = true
		}
		if props, ok := record.InputSchema[keyProperties].(map[string]any); ok {
			for key := range props {
				if requiredSet[key] {
					continue // Agent must fill required fields
				}
				if record.ZeroValues != nil {
					if zv, ok := record.ZeroValues[key]; ok {
						tmplArgs[key] = zv
					}
				}
			}
		}
		// Merge agent's existing valid arguments into the template
		maps.Copy(tmplArgs, args)
		correction["call_template"] = map[string]any{
			keyURN:             urn,
			keyArguments:       tmplArgs,
			"required_missing": missing,
		}
		// 🛡️ COMPACT SCHEMA: Only include properties relevant to missing fields
		// to prevent full-schema bloat in validation error responses.
		compactProps := make(map[string]any)
		if props, ok := record.InputSchema[keyProperties].(map[string]any); ok {
			for _, key := range missing {
				if propDef, ok := props[key]; ok {
					compactProps[key] = propDef
				}
			}
		}
		correction["schema"] = map[string]any{
			keyType:       schemaTypeObject,
			keyProperties: compactProps,
			keyRequired:   missing,
		}
	}

	telemetry.ProxyOptimization.Tier2StructuredErrs.Add(1)
	corrJSON := marshalIndentOrEmpty(correction)
	return fmt.Errorf("[VALIDATION_ERROR]: %w\n\n```json\n%s\n```", baseErr, string(corrJSON))
}

// ResolveURN parses and validates a tool URN, performing auto-resolution
// if the initial URN doesn't match a known tool.
// Returns the canonical server name, tool name, resolved URN, and the ToolRecord (if found).
func (ps *ProxyService) ResolveURN(ctx context.Context, inputURN string) (server, tool, resolvedURN string, record *db.ToolRecord, err error) {
	urn := rawURN(inputURN)
	parts := strings.Split(urn, ":")
	if len(parts) < 2 {
		slog.Warn("gateway: invalid URN format", "op", "ResolveURN", keyURN, inputURN)
		return "", "", "", nil, fmt.Errorf("invalid URN")
	}

	switch {
	case len(parts) == 2:
		server, tool = parts[0], parts[1]
	default:
		// 3+ segments: the first is the server and the last is the tool; any middle
		// segments are category qualifiers. PROXY-L1: ≥4-part URNs previously
		// resolved to the wrong tool (parts[0]:parts[1]).
		server, tool = parts[0], parts[len(parts)-1]
		urn = fmt.Sprintf("%s:%s", server, tool)
	}

	// Internal tools don't need DB validation
	if server == serverMagictools {
		return server, tool, urn, nil, nil
	}

	// Pre-call validation: ensure the tool exists before attempting proxy
	record, getErr := ps.Handler.Store.GetTool(urn)
	if getErr != nil {
		// Server-scoped auto-resolve: never silently remap to a different server.
		serverSpecific, searchErr := ps.Handler.Store.SearchTools(ctx, tool, "", server, 0.0, ps.Handler.Config.ScoreFusionAlpha, db.DomainSystem, false)
		if searchErr != nil && !isSearchNoMatchErr(searchErr) {
			slog.Log(ctx, util.LevelTrace, "gateway: auto-resolve search logic partial error", keyError, searchErr)
		}
		if len(serverSpecific) > 0 {
			for _, sug := range serverSpecific {
				if strings.EqualFold(sug.Name, tool) {
					slog.Info("gateway: auto-resolved tool on same server", "original", inputURN, "resolved", sug.URN)
					return sug.Server, tool, sug.URN, sug, nil
				}
			}
			return "", "", "", nil, fmt.Errorf("tool URN %q not found on server %q. Did you mean %q?", inputURN, server, serverSpecific[0].URN)
		}
		return "", "", "", nil, fmt.Errorf("tool URN %q not found on server %q; call %s to discover available capabilities", inputURN, server, toolAlignTools)
	}

	return server, tool, urn, record, nil
}

// EnsureServerReady performs lazy activation of a sub-server if it's not currently running.
// Uses singleflight to coalesce multiple concurrent requests that hit a sleeping server.
func (ps *ProxyService) EnsureServerReady(ctx context.Context, server string) (bootLatencyMs int64, err error) {
	startBoot := time.Now()
	if _, ok := ps.Handler.Registry.GetServerSession(server); !ok {
		_, doErr, _ := ps.Handler.LazyBootGroup.Do(server, func() (any, error) {
			for _, sc := range ps.Handler.Config.GetManagedServers() {
				if sc.Name == server {
					if err := ps.Handler.Registry.Connect(ctx, sc.Name, sc.Command, sc.Args, sc.Env, sc.Hash()); err != nil {
						return nil, err
					}
					return nil, nil
				}
			}
			return nil, fmt.Errorf("server %s not found in managed config", server)
		})
		if doErr != nil {
			slog.Error("gateway: lazy activation failed", "server", server, keyError, doErr)
			ps.Handler.Telemetry.RecordFault(server)
			telemetry.IPCSessionCounters.Readiness503s.Add(1)
			return time.Since(startBoot).Milliseconds(), doErr
		}
	}
	return time.Since(startBoot).Milliseconds(), nil
}

// ExecuteProxy calls a tool on a sub-server via the registry proxy.
func (ps *ProxyService) ExecuteProxy(ctx context.Context, server, tool string, arguments map[string]any, timeout time.Duration) (*mcp.CallToolResult, error) {
	res, err := ps.Handler.Registry.CallProxy(ctx, server, tool, arguments, timeout)
	if err != nil {
		slog.Log(ctx, util.LevelTrace, "gateway: call_proxy failure", "server", server, "tool", tool, keyError, err)
		return nil, err
	}
	slog.Log(ctx, util.LevelTrace, "gateway: call_proxy success", "server", server, "tool", tool)
	return res, nil
}

// PostProcessResponse applies the shared response pipeline stages that should
// be applied to all proxy call results, whether routed via call_proxy or
// via the CallToolMiddleware namespaced path. This ensures consistent
// minification, size caps, and telemetry across both execution paths.
//
// Stages included:
//  1. Orchestrator signal telemetry stripping (__orchestrator_signal)
//  2. Token aggregation (token_spend)
//  3. Soft failure inspection (InspectResponse)
//  4. Squeeze minification (MinifyResponse) — respecting bypass_minification arg
//  5. 24KB center-truncation safety cap (CenterTruncate)
//  6. _diagnostics meta enrichment (squeeze ratio, raw/post bytes)
func (ps *ProxyService) PostProcessResponse(
	ctx context.Context,
	res *mcp.CallToolResult,
	server, tool, urn string,
	toolRecord *db.ToolRecord,
	args map[string]any,
) (*mcp.CallToolResult, error) {
	if res == nil {
		return nil, nil
	}
	_ = toolRecord

	bypassMinification, truncateLimit := ps.resolveBypassMinification(urn, args)
	res.Content = ps.stripOrchestratorSignals(res.Content, urn)

	var structBytes []byte
	if res.StructuredContent != nil {
		structBytes = marshalJSONOrEmpty(res.StructuredContent)
	}

	if diag := ps.InspectResponse(ctx, res, server, tool, structBytes); diag != nil && diag.Detected {
		if res.Meta == nil {
			res.Meta = make(map[string]any)
		}
		res.Meta["soft_failure"] = diag
		slog.Warn("gateway: soft failure detected", "server", server, "tool", tool, "reason", diag.Reason)
		ps.Handler.Telemetry.RecordSoftFailure(server)
	}

	rawSize := measureResponseSize(res, structBytes)
	sentSize := int64(0)
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			sentSize = int64(len(raw))
		}
	}

	if util.IsInternal(ctx) {
		slog.Debug("gateway: routing internal JSON-RPC response", "server", server, "tool", tool)
		ps.Handler.Telemetry.AddBytes(server, sentSize, rawSize, rawSize)
		return res, nil
	}

	res, postSize := ps.applyMinificationStage(ctx, res, server, tool, bypassMinification, sentSize, truncateLimit, structBytes)
	applyResponseByteCap(res, bypassMinification)
	enrichResponseDiagnostics(res, rawSize, postSize, bypassMinification)

	return res, nil
}

// MinifyResponse applies the data hardening pipeline to a proxy result:
// squeeze nulls, truncate large strings, transform to hybrid markdown,
// and attach the raw-data retrieval footer.
// MinifyResponse reduces a structured response to hybrid markdown. PROXY-M5: if
// rawMarshaled is non-nil it is reused as the pre-mutation marshal of
// res.StructuredContent (the caller already produced it), avoiding a redundant
// full re-marshal of a potentially large payload. Pass nil to marshal internally.
func (ps *ProxyService) MinifyResponse(ctx context.Context, res *mcp.CallToolResult, server, tool string, sentSize int64, truncateLimit int, rawMarshaled []byte) *mcp.CallToolResult {
	if res.IsError || res.StructuredContent == nil {
		return res
	}

	// A. Cache the RAW full version before any reduction.
	callID := fmt.Sprintf("%s-full-%d", tool, time.Now().UnixNano())
	rawBytes := rawMarshaled
	if rawBytes == nil {
		var rawErr error
		if rawBytes, rawErr = json.Marshal(res.StructuredContent); rawErr != nil {
			slog.Warn("gateway: failed to serialize raw response caching payload", keyError, rawErr)
		}
	}
	preLen := int64(len(rawBytes))
	submitBackground(func() {
		if ctx.Err() != nil {
			return // Context cancelled; store may be closing
		}
		telemetry.OptMetrics.CSSAOffloadBytes.Add(int64(len(rawBytes)))
		if err := ps.Handler.Store.SaveRawResource(callID, rawBytes); err != nil {
			slog.Warn("gateway: failed to session-cache raw response", "call_id", callID, keyError, err)
		}
	})

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.Debug("gateway: pre-transform raw output", "server", server, "tool", tool, "size", len(rawBytes), "content", string(rawBytes))
	}

	// Hybrid JSON markdown_payload fast-path: if the StructuredContent contains
	// a pre-rendered GFM markdown payload, surface it directly as TextContent.
	// This bypasses transformToHybrid which would strip the markdown into a generic
	// JSON code block, making reports unreadable to the AI agent.
	if mdPayload := extractMarkdownPayload(rawBytes); mdPayload != "" {
		slog.Info("gateway: hybrid markdown_payload fast-path activated",
			"server", server, "tool", tool, "md_size", len(mdPayload))

		postLen := int64(len(mdPayload))
		ps.Handler.Telemetry.AddBytes(server, sentSize, preLen, postLen)

		md := mdPayload + fmt.Sprintf("\n\n[Full raw output available: mcp://magictools/raw/%s]", callID)
		res.Content = append([]mcp.Content{&mcp.TextContent{Text: md}}, res.Content...)
		res.StructuredContent = nil
		return res
	}

	// Phase 2: Small-response fast-path — skip squeeze/truncate for payloads < 1KB
	if preLen < int64(config.Proxy.SmallResponseThreshold) {
		slog.Log(ctx, util.LevelTrace, "gateway: fast-path (small response)", "server", server, "tool", tool, "size", preLen)

		minifiedData, miniErr := json.MarshalIndent(res.StructuredContent, "", "  ")
		if miniErr != nil {
			slog.Warn("gateway: failed to marshal for hybrid minification", keyError, miniErr)
		}
		md := transformToHybrid(minifiedData, ps.Handler.Config.MaxResponseTokens)

		postLen := int64(len(md))
		ps.Handler.Telemetry.AddBytes(server, sentSize, preLen, postLen)

		md += fmt.Sprintf("\n\n[Full raw output available: mcp://magictools/raw/%s]", callID)
		res.Content = append([]mcp.Content{&mcp.TextContent{Text: md}}, res.Content...)
		res.StructuredContent = nil
		return res
	}

	// B. Single-pass squeeze + truncate: remove nulls/empty arrays AND center-truncate large strings (>2000 chars)
	telemetry.OptMetrics.SqueezeTruncations.Add(1)
	res.StructuredContent = util.SqueezeAndTruncate(res.StructuredContent, truncateLimit)

	processedBytes, procErr := json.Marshal(res.StructuredContent)
	if procErr == nil {
		postLen := int64(len(processedBytes))
		telemetry.OptMetrics.TotalRawBytes.Add(preLen)
		telemetry.OptMetrics.TotalSqueezedBytes.Add(postLen)
		if slog.Default().Enabled(ctx, slog.LevelDebug) {
			slog.Debug("gateway: post-squeeze-truncate output", "server", server, "tool", tool, "pre_size", preLen, "post_size", postLen, "content", string(processedBytes))
		}
	}

	// D. Transform to Hybrid Markdown
	minifiedData, miniErr := json.MarshalIndent(res.StructuredContent, "", "  ")
	if miniErr != nil {
		slog.Warn("gateway: failed to marshal structured content to markdown", keyError, miniErr)
	}
	md := transformToHybrid(minifiedData, ps.Handler.Config.MaxResponseTokens)

	postLen := int64(len(md))
	ps.Handler.Telemetry.AddBytes(server, sentSize, preLen, postLen)

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.Debug("gateway: post-minifier markdown", "server", server, "tool", tool, "size", len(md), "content", md)
	}

	// Append the retrieval footer for cached raw data
	md += fmt.Sprintf("\n\n[Full raw output available: mcp://magictools/raw/%s]", callID)

	res.Content = append([]mcp.Content{&mcp.TextContent{Text: md}}, res.Content...)
	res.StructuredContent = nil

	return res
}

// SoftFailureDiagnostic describes a tool response that succeeded at the RPC
// level but returned suspiciously empty or zero-result data.
type SoftFailureDiagnostic struct {
	Detected   bool   `json:"detected"`
	Reason     string `json:"reason"`
	Server     string `json:"server"`
	Tool       string `json:"tool"`
	Suggestion string `json:"suggestion"`
}

// InspectResponse examines a successful proxy result for soft-failure patterns.
// It returns nil when the response looks normal (fast exit for the happy path).
func (ps *ProxyService) InspectResponse(ctx context.Context, res *mcp.CallToolResult, server, tool string, structBytes []byte) *SoftFailureDiagnostic {
	if res == nil || res.IsError {
		return nil
	}

	// Inspect StructuredContent (JSON payload) when present
	if structBytes != nil {
		if diag := inspectJSON(structBytes, server, tool); diag != nil {
			return diag
		}
	} else if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err == nil {
			if diag := inspectJSON(raw, server, tool); diag != nil {
				return diag
			}
		}
	}

	// Inspect text content as a fallback (some servers return JSON in text)
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok || tc.Text == "" {
			continue
		}
		// Only attempt JSON parse if it looks like JSON
		trimmed := strings.TrimSpace(tc.Text)
		if trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[') {
			if diag := inspectJSON([]byte(trimmed), server, tool); diag != nil {
				return diag
			}
		}
	}

	return nil
}

// inspectJSON checks a raw JSON payload for soft-failure indicators.
func inspectJSON(raw []byte, server, tool string) *SoftFailureDiagnostic {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	// Check nested "data" wrapper (common pattern: {data: {results: [], metadata: {total_count: 0}}})
	if nested, ok := payload["data"].(map[string]any); ok {
		if diag := checkEmptyResults(nested, server, tool); diag != nil {
			return diag
		}
	}

	return checkEmptyResults(payload, server, tool)
}

// checkEmptyResults inspects a JSON object for empty-result patterns.
func checkEmptyResults(m map[string]any, server, tool string) *SoftFailureDiagnostic {
	// Pattern 1: "results" key is an empty array
	if results, ok := m["results"]; ok {
		if arr, isArr := results.([]any); isArr && len(arr) == 0 {
			return &SoftFailureDiagnostic{
				Detected:   true,
				Reason:     "results array is empty",
				Server:     server,
				Tool:       tool,
				Suggestion: "The tool returned successfully but with zero results. Consider retrying with different parameters or checking the sub-server logs.",
			}
		}
	}

	// Pattern 2: Count-like keys equal to zero
	countKeys := []string{"total_count", keyResultCount, "count", "totalCount", "resultCount"}
	for _, key := range countKeys {
		if v, ok := m[key]; ok {
			if isZeroNumeric(v) {
				return &SoftFailureDiagnostic{
					Detected:   true,
					Reason:     fmt.Sprintf("%s is 0", key),
					Server:     server,
					Tool:       tool,
					Suggestion: "The tool reported zero matching entries. Verify the query parameters or check sub-server connectivity.",
				}
			}
		}

		// Also check inside keyMetadata wrapper
		if meta, ok := m[keyMetadata].(map[string]any); ok {
			if v, ok := meta[key]; ok {
				if isZeroNumeric(v) {
					return &SoftFailureDiagnostic{
						Detected:   true,
						Reason:     fmt.Sprintf("metadata.%s is 0", key),
						Server:     server,
						Tool:       tool,
						Suggestion: "The tool reported zero matching entries. Verify the query parameters or check sub-server connectivity.",
					}
				}
			}
		}
	}

	return nil
}

// isZeroNumeric checks whether a JSON-decoded value is numerically zero.
func isZeroNumeric(v any) bool {
	switch n := v.(type) {
	case float64:
		return n == 0
	case int:
		return n == 0
	case int64:
		return n == 0
	}
	return false
}

// snapToEnum uses Levenshtein distance to automatically "snap" minor enum hallucinations
// to valid schema bounds. This minimizes orchestrator retries for trivial mismatches.
func (ps *ProxyService) snapToEnum(val string, enum []any) string {
	if len(enum) == 0 {
		return val
	}
	minDist := 100
	bestMatch := val
	lowerVal := strings.ToLower(val)

	for _, e := range enum {
		if s, ok := e.(string); ok {
			if strings.EqualFold(s, val) {
				return s // Perfect match (case-insensitive)
			}
			dist := util.LevenshteinDistance(lowerVal, strings.ToLower(s))
			if dist < minDist {
				minDist = dist
				bestMatch = s
			}
		}
	}

	// Only snap if the distance is small (heuristic: distance <= 2 or < 30% of target length)
	if minDist <= 2 || float64(minDist) < float64(len(bestMatch))*0.3 {
		return bestMatch
	}
	return val
}

// stripTrailingCommas removes a comma immediately preceding a closing } or ]
// that lies OUTSIDE a JSON string literal. PROXY-M3: a raw regex would also
// mutate a "," + "}" sequence inside a string value, corrupting valid data.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop the trailing comma
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// appendMissingCloseBraces appends '}' for '{' left unbalanced OUTSIDE string
// literals. PROXY-M3: counting braces inside string values appended spurious '}'.
func appendMissingCloseBraces(s string) string {
	depth := 0
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		}
	}
	if depth > 0 {
		return s + strings.Repeat("}", depth)
	}
	return s
}

// repairJSONHeuristic attempts structural repairs on malformed JSON strings
// before the final validation pass. PROXY-M3: it never mutates already-valid
// JSON, and the brace/comma repairs are string-literal-aware.
func (ps *ProxyService) repairJSONHeuristic(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return input
	}

	// 1. Handle Markdown code block escapes
	if after, ok := strings.CutPrefix(trimmed, "```json"); ok {
		trimmed = strings.TrimSuffix(after, "```")
	} else if after, ok := strings.CutPrefix(trimmed, "```"); ok {
		trimmed = strings.TrimSuffix(after, "```")
	}
	trimmed = strings.TrimSpace(trimmed)

	// 2. PROXY-M3: if it is already valid JSON, return it untouched.
	if json.Valid([]byte(trimmed)) {
		return trimmed
	}

	// 3. String-literal-aware structural repairs.
	trimmed = appendMissingCloseBraces(trimmed)
	trimmed = stripTrailingCommas(trimmed)
	return trimmed
}

// ---------------------------------------------------------------------------
// CallProxy argument repair pipeline
// ---------------------------------------------------------------------------

// callProxyParams holds the deserialized parameters for the call_proxy handler.
type callProxyParams struct {
	URN                string         `json:"urn"`
	Arguments          map[string]any `json:"arguments"`
	BypassMinification bool           `json:"bypass_minification"`
}

// RepairResult describes what repair was applied to malformed arguments.
type RepairResult struct {
	Repaired   bool   // true if any repair was applied
	RepairType string // "double_encoded", repairTypeXMLStripped, "flat_structure", "markdown_unwrap", "trailing_comma", "nested_unwrap", "heuristic"
}

// unmarshalCallProxyArgs attempts to unmarshal call_proxy arguments, applying
// a multi-phase repair pipeline on failure. This eliminates the need for
// inline goto-based error recovery in the handler.
func (ps *ProxyService) unmarshalCallProxyArgs(raw json.RawMessage) (callProxyParams, error) {
	// Happy path: direct unmarshal succeeds
	var params callProxyParams
	if err := json.Unmarshal(raw, &params); err == nil {
		return params, nil
	}

	// Repair path
	telemetry.ArgumentRepairs.TotalAttempts.Add(1)
	repaired, result, repairErr := ps.repairCallProxyArgs(raw)
	if repairErr != nil {
		telemetry.ArgumentRepairs.TotalFailures.Add(1)
		slog.Error("gateway: all argument repair strategies exhausted",
			"tool", toolCallProxy,
			keyError, repairErr,
			"raw", string(raw))
		return callProxyParams{}, fmt.Errorf("failed to unmarshal arguments: %w", repairErr)
	}

	slog.Info("gateway: argument repair successful",
		"tool", toolCallProxy,
		"repair_type", result.RepairType,
		keyURN, repaired.URN)
	return repaired, nil
}

// repairCallProxyArgs applies a multi-phase repair pipeline to malformed
// call_proxy arguments. Each phase is tried in order; first success wins.
//
// Phase order:
//  1. Double-encoded string detection (with XML tag stripping)
//  2. Flat structure detection (arguments at top level alongside urn)
//  3. Nested unwrap (arguments.arguments)
//  4. Trailing comma repair
//  5. Legacy heuristic (markdown code blocks, missing braces)
func (ps *ProxyService) repairCallProxyArgs(raw json.RawMessage) (callProxyParams, RepairResult, error) {
	if params, result, ok := ps.repairDoubleEncodedProxyArgs(raw); ok {
		return params, result, nil
	}

	var rawEnvelope rawCallProxyEnvelope

	// ── Phase 2: Flat structure detection ──
	// Agent puts inner arguments at the top level alongside `urn`.
	// e.g., {"urn": "...", "stage": "THESIS", "lemma": "test"}
	var flatMap map[string]any
	if json.Unmarshal(raw, &flatMap) == nil {
		urnVal, hasURN := flatMap[keyURN]
		if hasURN {
			urnStr := stringFrom(urnVal)
			bypass := boolFrom(flatMap["bypass_minification"])

			// 🛡️ BUG-04: Only collect flat structure if 'arguments' key is absent or not a valid map.
			// If it's already a map, Phase 3 nested unwrap handles it correctly.
			skipFlat := false
			if argsVal, hasArgs := flatMap[keyArguments]; hasArgs {
				if _, isMap := argsVal.(map[string]any); isMap {
					skipFlat = true
				}
			}

			if !skipFlat {
				// Collect non-envelope keys as arguments
				envelopeKeys := map[string]bool{keyURN: true, "bypass_minification": true}
				args := make(map[string]any)
				for k, v := range flatMap {
					if !envelopeKeys[k] {
						args[k] = v
					}
				}

				// Only trigger if there are extra keys beyond the envelope
				if len(args) > 0 && urnStr != "" {
					telemetry.ArgumentRepairs.FlatStructure.Add(1)
					return callProxyParams{
						URN:                urnStr,
						Arguments:          args,
						BypassMinification: bypass,
					}, RepairResult{Repaired: true, RepairType: "flat_structure"}, nil
				}
			}
		}
	}

	// ── Phase 3: Nested unwrap ──
	// {"arguments": {"arguments": {"stage": "THESIS"}}, "urn": "..."}
	if json.Unmarshal(raw, &rawEnvelope) == nil && len(rawEnvelope.Arguments) > 0 {
		var outerArgs struct {
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(rawEnvelope.Arguments, &outerArgs) == nil && outerArgs.Arguments != nil {
			telemetry.ArgumentRepairs.NestedUnwrap.Add(1)
			return callProxyParams{
				URN:                rawEnvelope.URN,
				Arguments:          outerArgs.Arguments,
				BypassMinification: rawEnvelope.BypassMinification,
			}, RepairResult{Repaired: true, RepairType: "nested_unwrap"}, nil
		}
	}

	// ── Phase 4: Trailing comma repair ──
	repairedStr := stripTrailingCommas(string(raw))
	var params callProxyParams
	if repairedStr != string(raw) {
		if json.Unmarshal([]byte(repairedStr), &params) == nil {
			telemetry.ArgumentRepairs.TrailingComma.Add(1)
			return params, RepairResult{Repaired: true, RepairType: "trailing_comma"}, nil
		}
	}

	// ── Phase 5: Legacy heuristic (markdown code blocks, missing braces) ──
	heuristicStr := ps.repairJSONHeuristic(string(raw))
	if json.Unmarshal([]byte(heuristicStr), &params) == nil {
		telemetry.ArgumentRepairs.Heuristic.Add(1)
		return params, RepairResult{Repaired: true, RepairType: "heuristic"}, nil
	}

	return callProxyParams{}, RepairResult{}, fmt.Errorf("all repair strategies exhausted")
}

// repairSimpleArgs applies a lightweight repair pipeline to handlers with
// simple struct arguments (string-typed fields). Handles markdown unwrap,
// trailing commas, and structural heuristics. Does NOT handle double-encoding
// since simple structs with string fields rarely trigger that failure mode.
func (ps *ProxyService) repairSimpleArgs(raw json.RawMessage, target any) error {
	telemetry.ArgumentRepairs.TotalAttempts.Add(1)

	// Trailing comma repair
	repairedStr := stripTrailingCommas(string(raw))
	if repairedStr != string(raw) {
		if json.Unmarshal([]byte(repairedStr), target) == nil {
			telemetry.ArgumentRepairs.TrailingComma.Add(1)
			slog.Info("gateway: simple args trailing comma repair successful")
			return nil
		}
	}

	// Legacy heuristic
	heuristicStr := ps.repairJSONHeuristic(string(raw))
	if json.Unmarshal([]byte(heuristicStr), target) == nil {
		telemetry.ArgumentRepairs.Heuristic.Add(1)
		slog.Info("gateway: simple args heuristic repair successful")
		return nil
	}

	telemetry.ArgumentRepairs.TotalFailures.Add(1)
	return fmt.Errorf("simple args repair exhausted")
}
