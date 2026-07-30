package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (ps *ProxyService) resolveBypassMinification(urn string, args map[string]any) (bool, int) {
	bypassMinification := false
	if bm, ok := args["bypass_minification"].(bool); ok {
		bypassMinification = bm
	}
	truncateLimit := config.Proxy.TruncateLimit
	if !bypassMinification && ps.Handler.Config != nil {
		for _, target := range ps.Handler.Config.GetSqueezeBypass() {
			if isTargetMatched(urn, target) {
				bypassMinification = true
				break
			}
		}
	}
	if !bypassMinification && args != nil {
		if tuningRaw, ok := args["__orchestrator_squeeze_tuning"]; ok {
			if tuning, isMap := tuningRaw.(map[string]any); isMap {
				if limitRaw, exists := tuning["truncate_limit"]; exists {
					if limitFloat, isNum := limitRaw.(float64); isNum {
						truncateLimit = int(limitFloat)
					}
				}
			}
		}
	}
	return bypassMinification, truncateLimit
}

func (ps *ProxyService) stripOrchestratorSignals(content []mcp.Content, urn string) []mcp.Content {
	if content == nil {
		return []mcp.Content{}
	}
	for _, c := range content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(tc.Text), &payload); err != nil {
			continue
		}
		if spendRaw, hasSpend := payload["token_spend"]; hasSpend {
			var spend int
			switch v := spendRaw.(type) {
			case float64:
				spend = int(v)
			case int:
				spend = v
			}
			if spend > 0 {
				telemetry.AddTokens(spend)
			}
		}
		if sigR, hasSig := payload["__orchestrator_signal"]; hasSig {
			if sig, ok := sigR.(map[string]any); ok {
				success := boolFrom(sig["success"])
				go func(u string, s bool) {
					if err := ps.Handler.Store.UpdateToolMetrics(u, s); err != nil {
						slog.Warn("gateway: failed to update tool metrics", keyURN, u, keyError, err)
					}
				}(urn, success)
			}
			delete(payload, "__orchestrator_signal")
			if stripped, err := json.Marshal(payload); err == nil {
				tc.Text = string(stripped)
			}
		}
	}
	return content
}

func (ps *ProxyService) applyMinificationStage(
	ctx context.Context,
	res *mcp.CallToolResult,
	server, tool string,
	bypassMinification bool,
	sentSize int64,
	truncateLimit int,
	structBytes []byte,
) (*mcp.CallToolResult, int64) {
	if bypassMinification {
		if res.Meta == nil {
			res.Meta = make(map[string]any)
		}
		res.Meta["bypass_minification"] = true
		rawSize := measureResponseSize(res, structBytes)
		ps.Handler.Telemetry.AddBytes(server, sentSize, rawSize, rawSize)
		if res.StructuredContent != nil {
			if rawJSON, marshalErr := json.MarshalIndent(res.StructuredContent, "", "  "); marshalErr == nil {
				md := fmt.Sprintf("```json\n%s\n```", string(rawJSON))
				res.Content = append([]mcp.Content{&mcp.TextContent{Text: md}}, res.Content...)
			}
		}
		return res, rawSize
	}
	rawSize := measureResponseSize(res, structBytes)
	res = ps.MinifyResponse(ctx, res, server, tool, sentSize, truncateLimit, structBytes)
	var postSize int64
	if res.StructuredContent == nil && rawSize > 0 {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				postSize += int64(len(tc.Text))
			}
		}
		if postSize == rawSize {
			ps.Handler.Telemetry.AddBytes(server, sentSize, rawSize, rawSize)
		}
		return res, postSize
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			postSize += int64(len(tc.Text))
		}
	}
	return res, postSize
}

func applyResponseByteCap(res *mcp.CallToolResult, bypassMinification bool) {
	byteCap := config.Proxy.MaxResponseBytes
	if bypassMinification {
		byteCap = config.Proxy.MaxResponseBytes * 8
		if byteCap <= 0 {
			byteCap = 1 << 20
		}
	}
	if byteCap <= 0 {
		return
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok && len(tc.Text) > byteCap {
			tc.Text = util.CenterTruncate(tc.Text, byteCap)
		}
	}
}

func enrichResponseDiagnostics(res *mcp.CallToolResult, rawSize, postSize int64, bypassMinification bool) {
	if res.Meta == nil {
		res.Meta = make(map[string]any)
	}
	squeezeRatio := float64(1)
	if rawSize > 0 {
		squeezeRatio = float64(postSize) / float64(rawSize)
	}
	res.Meta["_diagnostics"] = map[string]any{
		"raw_bytes":     rawSize,
		"post_bytes":    postSize,
		"squeeze_ratio": squeezeRatio,
		"minified":      !bypassMinification,
	}
}
