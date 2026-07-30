package handler

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func (ps *ProxyService) injectZeroValues(record *db.ToolRecord, args map[string]any) {
	if record.ZeroValues == nil {
		return
	}
	for key, zeroVal := range record.ZeroValues {
		if _, exists := args[key]; !exists {
			args[key] = zeroVal
		}
	}
}

func (ps *ProxyService) coerceSchemaTypes(record *db.ToolRecord, args map[string]any) {
	if record.InputSchema == nil {
		return
	}
	props, ok := record.InputSchema[keyProperties].(map[string]any)
	if !ok {
		return
	}
	for key, val := range args {
		propDef, exists := props[key].(map[string]any)
		if !exists {
			continue
		}
		typ, hasTyp := propDef[keyType].(string)
		if !hasTyp {
			continue
		}
		ps.coercePropertyType(key, val, typ, propDef, args)
	}
}

func (ps *ProxyService) coercePropertyType(key string, val any, typ string, propDef map[string]any, args map[string]any) {
	switch typ {
	case schemaTypeInteger:
		coerceIntegerArg(args, key, val)
	case "number":
		coerceNumberArg(args, key, val)
	case schemaTypeString:
		coerceStringArg(ps, args, key, val, propDef)
	case schemaTypeArray:
		coerceArrayArg(args, key, val)
	}
}

func coerceIntegerArg(args map[string]any, key string, val any) {
	switch v := val.(type) {
	case float64:
		if v == math.Trunc(v) {
			args[key] = int(v)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			args[key] = i
		}
	}
}

func coerceNumberArg(args map[string]any, key string, val any) {
	str, ok := val.(string)
	if !ok {
		return
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(str), 64); err == nil {
		args[key] = f
	}
}

func coerceStringArg(ps *ProxyService, args map[string]any, key string, val any, propDef map[string]any) {
	switch v := val.(type) {
	case float64, int, bool:
		args[key] = fmt.Sprintf("%v", v)
	case string:
		if enum, ok := propDef["enum"].([]any); ok && len(enum) > 0 {
			snapped := ps.snapToEnum(v, enum)
			if snapped != v {
				slog.Info("gateway: enum snapped", "field", key, "original", v, "snapped", snapped)
				args[key] = snapped
			}
		}
	}
}

func coerceArrayArg(args map[string]any, key string, val any) {
	if val == nil {
		args[key] = []any{}
	}
}
