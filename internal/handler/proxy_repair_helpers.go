package handler

import (
	"encoding/json"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

type rawCallProxyEnvelope struct {
	URN                string          `json:"urn"`
	Arguments          json.RawMessage `json:"arguments"`
	BypassMinification bool            `json:"bypass_minification"`
}

func (ps *ProxyService) repairDoubleEncodedProxyArgs(raw json.RawMessage) (callProxyParams, RepairResult, bool) {
	var rawEnvelope rawCallProxyEnvelope
	if json.Unmarshal(raw, &rawEnvelope) != nil || len(rawEnvelope.Arguments) == 0 {
		return callProxyParams{}, RepairResult{}, false
	}
	argStr := strings.TrimSpace(string(rawEnvelope.Arguments))
	if argStr == "" || argStr[0] != '"' {
		return callProxyParams{}, RepairResult{}, false
	}
	var innerStr string
	if json.Unmarshal(rawEnvelope.Arguments, &innerStr) != nil {
		return callProxyParams{}, RepairResult{}, false
	}
	repairType := "double_encoded"
	if idx := strings.Index(innerStr, "\n<parameter "); idx > 0 {
		innerStr = innerStr[:idx]
		repairType = repairTypeXMLStripped
		telemetry.ArgumentRepairs.XMLStripped.Add(1)
	} else if idx := strings.Index(innerStr, "<parameter "); idx > 0 {
		innerStr = innerStr[:idx]
		repairType = repairTypeXMLStripped
		telemetry.ArgumentRepairs.XMLStripped.Add(1)
	}
	innerStr = ps.repairJSONHeuristic(innerStr)
	var innerArgs map[string]any
	if json.Unmarshal([]byte(innerStr), &innerArgs) != nil {
		return callProxyParams{}, RepairResult{}, false
	}
	telemetry.ArgumentRepairs.DoubleEncoded.Add(1)
	return callProxyParams{
		URN:                rawEnvelope.URN,
		Arguments:          innerArgs,
		BypassMinification: rawEnvelope.BypassMinification,
	}, RepairResult{Repaired: true, RepairType: repairType}, true
}
