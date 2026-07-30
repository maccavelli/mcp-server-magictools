package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcplib"
)

func loadRawToolList(srv *SubServer, serverName string) map[string]any {
	if srv == nil || srv.Filter == nil {
		return nil
	}
	raw := srv.Filter.GetLastFrame()
	if len(raw) == 0 {
		return nil
	}
	var rawList map[string]any
	if err := json.Unmarshal(raw, &rawList); err != nil {
		slog.Warn("sync: failed to unmarshal raw tool list", "component", "proxy", "server_id", serverName, "error", err)
		return nil
	}
	return rawList
}

func (m *WarmRegistry) normalizeToolsBeforeSave(sc config.ServerConfig, srv *SubServer, tools *mcp.ListToolsResult, rawList map[string]any) {
	for i, t := range tools.Tools {
		if override := m.Config.GetToolOverride(sc.Name, t.Name); override != "" {
			t.Description = override
			slog.Info("sync: applied pure config description override", "server", sc.Name, "tool", t.Name)
		}
		recoverLegacyToolSchema(t, rawList, i, sc.Name)
		m.warnAndRepairToolSchema(sc, srv, t)
	}
}

func recoverLegacyToolSchema(t *mcp.Tool, rawList map[string]any, index int, serverName string) {
	if t.InputSchema != nil || rawList == nil {
		return
	}
	result, ok := rawList["result"].(map[string]any)
	if !ok {
		return
	}
	rTools, ok := result["tools"].([]any)
	if !ok || index >= len(rTools) {
		return
	}
	rTool, ok := rTools[index].(map[string]any)
	if !ok {
		return
	}
	params, ok := rTool["parameters"]
	if !ok || params == nil {
		return
	}
	slog.Info("sync: recovered legacy 'parameters' field", "component", "proxy", "server_id", serverName, "tool", t.Name)
	t.InputSchema = params
}

func (m *WarmRegistry) warnAndRepairToolSchema(sc config.ServerConfig, srv *SubServer, t *mcp.Tool) {
	if err := util.ValidateZeroNull(t); err != nil {
		var raw []byte
		if srv != nil && srv.Filter != nil {
			raw = srv.Filter.GetLastFrame()
		}
		slog.Warn("Sync Warning: Schema issues detected but will attempt repair",
			"server", sc.Name, "tool", t.Name, "error", err, "raw_json", string(raw))
		m.Logger.Log(logging.WARNING, sc.Name, fmt.Sprintf("Schema Warning: %v (Attempting Repair)", err))
	}
	m.autoPromoteRequiredFields(sc, t)
}

func (m *WarmRegistry) autoPromoteRequiredFields(sc config.ServerConfig, t *mcp.Tool) {
	schemaMap := m.toSchemaMap(t.InputSchema)
	if schemaMap == nil {
		return
	}
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok || len(props) == 0 {
		return
	}
	if _, hasReq := schemaMap["required"]; hasReq {
		return
	}
	promoted := promotedRequiredFields(props)
	if len(promoted) == 0 {
		return
	}
	schemaMap["required"] = promoted
	t.InputSchema = schemaMap
	slog.Warn("sync: AUTO-PROMOTED fields to required (no default, non-nullable)",
		"server", sc.Name, "tool", t.Name,
		"promoted_count", len(promoted), "fields", promoted)
	m.Logger.Log(logging.WARNING, sc.Name,
		fmt.Sprintf("Schema Auto-Repair: %s — promoted %d fields to required", t.Name, len(promoted)))
}

func promotedRequiredFields(props map[string]any) []any {
	var promoted []any
	for fieldName, fieldDefRaw := range props {
		if mcplib.IsOptionalTelemetryField(fieldName) {
			continue
		}
		fieldDef, ok := fieldDefRaw.(map[string]any)
		if !ok {
			continue
		}
		if _, hasDefault := fieldDef["default"]; hasDefault {
			continue
		}
		if schemaFieldNullable(fieldDef) {
			continue
		}
		promoted = append(promoted, fieldName)
	}
	return promoted
}

func schemaFieldNullable(fieldDef map[string]any) bool {
	typeVal, ok := fieldDef["type"]
	if !ok {
		return false
	}
	switch tv := typeVal.(type) {
	case string:
		return tv == "null"
	case []any:
		for _, nullCheck := range tv {
			if s, ok := nullCheck.(string); ok && s == "null" {
				return true
			}
		}
	}
	return false
}

func disabledToolSet(sc config.ServerConfig) map[string]bool {
	disabled := make(map[string]bool, len(sc.DisabledTools))
	for _, t := range sc.DisabledTools {
		disabled[t] = true
	}
	return disabled
}

func (m *WarmRegistry) scheduleIntelligenceGC(ctx context.Context, sc config.ServerConfig, tools *mcp.ListToolsResult, disabled map[string]bool) {
	var validURNs []string
	for _, t := range tools.Tools {
		if !disabled[t.Name] {
			validURNs = append(validURNs, fmt.Sprintf("%s:%s", sc.Name, util.SanitizeToolSchema(t).Name))
		}
	}
	go func(c context.Context, server string, urns []string) {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("sync: intelligence GC panicked", "server", server, "panic", r)
			}
		}()
		if c.Err() != nil {
			return
		}
		if err := m.Store.PruneOrphanedIntelligence(server, urns); err != nil {
			slog.Warn("sync: background sweep of orphaned intelligence failed", "server", server, "error", err)
		}
	}(ctx, sc.Name, validURNs)
}

func (m *WarmRegistry) buildSyncBatchRecords(sc config.ServerConfig, tools *mcp.ListToolsResult, disabled map[string]bool) ([]*db.ToolRecord, map[string]map[string]any, int) {
	var batchRecords []*db.ToolRecord
	batchSchemas := make(map[string]map[string]any)
	indexed := 0
	for _, t := range tools.Tools {
		if disabled[t.Name] {
			continue
		}
		rec, schemaMap := m.buildToolRecord(sc, t)
		batchRecords = append(batchRecords, rec)
		batchSchemas[rec.SchemaHash] = schemaMap
		indexed++
	}
	return batchRecords, batchSchemas, indexed
}

func (m *WarmRegistry) buildToolRecord(sc config.ServerConfig, t *mcp.Tool) (*db.ToolRecord, map[string]any) {
	st := util.SanitizeToolSchema(t)
	h := m.hashSchema(st)
	record := &db.ToolRecord{
		URN:          fmt.Sprintf("%s:%s", sc.Name, st.Name),
		Name:         st.Name,
		Server:       sc.Name,
		Description:  st.Description,
		LiteSummary:  m.minifyDescription(st.Description),
		Intent:       m.extractIntent(st.Name, st.Description),
		SchemaHash:   h,
		LastSyncedAt: time.Now().Unix(),
		Category:     m.deriveCategory(sc.Name),
		IsNative:     sc.Name == serverMagictools,
	}
	hydrateRoleAndPhase(record)
	schemaMap := m.toSchemaMap(st.InputSchema)
	record.ParameterNames = parameterNamesFromSchema(schemaMap)
	record.EnumValues = enumValuesFromSchema(schemaMap)
	if schemaMap != nil {
		record.ZeroValues = db.ComputeZeroValues(schemaMap)
	}
	return record, schemaMap
}

func parameterNamesFromSchema(schemaMap map[string]any) []string {
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func enumValuesFromSchema(schemaMap map[string]any) []string {
	var enums []string
	var walkSchema func(any)
	walkSchema = func(node any) {
		switch val := node.(type) {
		case map[string]any:
			if enumArr, ok := val["enum"].([]any); ok {
				for _, ev := range enumArr {
					if s, ok := ev.(string); ok && s != "" {
						enums = append(enums, s)
					}
				}
			}
			for _, v := range val {
				walkSchema(v)
			}
		case []any:
			for _, v := range val {
				walkSchema(v)
			}
		}
	}
	walkSchema(schemaMap)
	if len(enums) == 0 {
		return nil
	}
	sort.Strings(enums)
	dedup := enums[:0]
	for i, e := range enums {
		if i == 0 || enums[i-1] != e {
			dedup = append(dedup, e)
		}
	}
	return dedup
}

func compositeSyncHash(batchRecords []*db.ToolRecord) string {
	sort.Slice(batchRecords, func(i, j int) bool {
		return batchRecords[i].URN < batchRecords[j].URN
	})
	hasher := sha256.New()
	for _, rec := range batchRecords {
		hasher.Write([]byte(rec.SchemaHash))
	}
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func (m *WarmRegistry) shouldSkipUnchangedSync(serverName, compositeHash string, batchRecords []*db.ToolRecord) bool {
	if m.Store.GetServerSyncHash(serverName) != compositeHash || len(batchRecords) == 0 {
		return false
	}
	if m.Store.HasServerTools(serverName) {
		slog.Info("sync: tool list unchanged, skipping purge+save", "server", serverName, "tools", len(batchRecords))
		return true
	}
	slog.Warn("sync: hash gate stale — tools missing from DB despite matching hash, forcing re-index",
		"server", serverName, "tools", len(batchRecords))
	return false
}

func (m *WarmRegistry) persistSyncedTools(ctx context.Context, sc config.ServerConfig, batchRecords []*db.ToolRecord, batchSchemas map[string]map[string]any, compositeHash string) error {
	if err := m.Store.BatchSaveTools(batchRecords, batchSchemas); err != nil {
		slog.Warn("sync: failed to batch save tools", "server", sc.Name, "error", err)
		return fmt.Errorf("failed to batch save tools: %w", err)
	}
	m.Store.SaveServerSyncHash(sc.Name, compositeHash)
	m.scheduleStaleToolPurge(ctx, sc.Name, batchRecords)
	m.scheduleSemanticHydration(ctx, sc.Name, batchRecords, batchSchemas)
	return nil
}

func (m *WarmRegistry) scheduleStaleToolPurge(ctx context.Context, server string, batchRecords []*db.ToolRecord) {
	currentURNs := make([]string, 0, len(batchRecords))
	for _, rec := range batchRecords {
		currentURNs = append(currentURNs, rec.URN)
	}
	go func(c context.Context, srv string, urns []string) {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("sync: stale tool purge panicked", "server", srv, "panic", r)
			}
		}()
		if c.Err() != nil {
			return
		}
		if err := m.Store.PurgeStaleServerTools(srv, urns); err != nil {
			slog.Warn("sync: background stale tool purge failed", "server", srv, "error", err)
		}
	}(ctx, server, currentURNs)
}

func (m *WarmRegistry) scheduleSemanticHydration(ctx context.Context, server string, records []*db.ToolRecord, schemas map[string]map[string]any) {
	llmConfigured := m.Config.Intelligence.Provider != "" && m.Config.Intelligence.APIKey != ""
	go func(c context.Context, recs []*db.ToolRecord, schemaMap map[string]map[string]any) {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("sync: semantic hydrator panicked (recovered)", "server", server, "error", r)
			}
		}()
		for _, rec := range recs {
			if c.Err() != nil {
				return
			}
			if llmConfigured {
				m.hydrateToolPendingLLM(rec, schemaMap)
			} else {
				m.hydrateToolStatic(rec, schemaMap)
			}
		}
	}(ctx, records, schemas)
}

func (m *WarmRegistry) hydrateToolPendingLLM(rec *db.ToolRecord, schemas map[string]map[string]any) {
	existing, err := m.Store.GetIntelligence(rec.URN)
	if err == nil && existing != nil && existing.AnalysisStatus == "hydrated" {
		if existing.SchemaHash == rec.SchemaHash {
			return
		}
		if existing.SchemaHash == "" {
			existing.SchemaHash = rec.SchemaHash
			if sErr := m.Store.SaveIntelligence(rec.URN, existing); sErr != nil {
				slog.Warn("sync: failed to migrate legacy schema hash", "urn", rec.URN, "error", sErr)
			} else {
				slog.Debug("sync: migrated legacy schema hash", "urn", rec.URN)
			}
			return
		}
	}
	intel := &db.ToolIntelligence{
		AnalysisStatus: "pending",
		Metrics: db.ToolMetrics{
			ProxyReliability: computeWBase(rec.Description, rec.Category, schemas[rec.SchemaHash], nil),
		},
	}
	if err := m.Store.SaveIntelligence(rec.URN, intel); err != nil {
		slog.Warn("sync: failed to mark tool as pending", "urn", rec.URN, "error", err)
		return
	}
	m.Store.PendingHydrations.Add(1)
	slog.Debug("sync: marked tool for LLM hydration", "urn", rec.URN)
}

func (m *WarmRegistry) hydrateToolStatic(rec *db.ToolRecord, schemas map[string]map[string]any) {
	existing, err := m.Store.GetIntelligence(rec.URN)
	if err == nil && existing != nil && existing.AnalysisStatus == hydratorVersion {
		if existing.SchemaHash == rec.SchemaHash {
			return
		}
		if existing.SchemaHash == "" {
			existing.SchemaHash = rec.SchemaHash
			if sErr := m.Store.SaveIntelligence(rec.URN, existing); sErr != nil {
				slog.Warn("sync: failed to migrate legacy schema hash", "urn", rec.URN, "error", sErr)
			}
			return
		}
	}
	category := rec.Category
	schema := schemas[rec.SchemaHash]
	negTriggers := generateNegativeTriggers(rec.Name, category)
	intel := &db.ToolIntelligence{
		AnalysisStatus:   hydratorVersion,
		SyntheticIntents: generateSyntheticIntents(rec.Name, rec.Description, category),
		LexicalTokens:    generateLexicalTokens(rec.Name, schema),
		NegativeTriggers: negTriggers,
		Metrics: db.ToolMetrics{
			ProxyReliability: computeWBase(rec.Description, category, schema, negTriggers),
		},
	}
	if err := m.Store.SaveIntelligence(rec.URN, intel); err != nil {
		slog.Warn("sync: failed to save static intelligence", "urn", rec.URN, "error", err)
		return
	}
	slog.Debug("sync: static hydrated tool intelligence", "urn", rec.URN, "wbase", intel.Metrics.ProxyReliability)
}
