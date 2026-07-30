package util

import (
	"crypto/rand"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"
)

// GenerateSessionID creates a high-entropy hex ID.
func GenerateSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// TruncateAllLargeStrings recursively finds and center-truncates any string field exceeding the limit.
// 🛡️ BASTION PERFORMANCE GUARD: Depth-limited (10 levels) to prevent recursion-based CPU spikes.
func TruncateAllLargeStrings(val any, limit int) any {
	return truncateRecursive(val, limit, 0)
}

func truncateRecursive(val any, limit int, depth int) any {
	if val == nil || depth > 10 {
		return val
	}

	switch v := val.(type) {
	case string:
		if len(v) > limit {
			return CenterTruncate(v, limit)
		}
		return v
	case map[string]any:
		if len(v) == 0 {
			return v
		}
		m := make(map[string]any, len(v))
		for k, mv := range v {
			m[k] = truncateRecursive(mv, limit, depth+1)
		}
		return m
	case []any:
		if len(v) == 0 {
			return v
		}
		res := make([]any, 0, len(v))
		for _, item := range v {
			res = append(res, truncateRecursive(item, limit, depth+1))
		}
		return res
	default:
		// 🛡️ Reflection fallback for complex pointers/structs
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			return truncateRecursive(rv.Elem().Interface(), limit, depth+1)
		}
		return val
	}
}

// CenterTruncate keeps parts of a string while inserting a visual marker.
// If it's a stack trace, it attempts to find and keep the context around the error.
func CenterTruncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	// 🛡️ Expert Heuristic: Stack Trace Awareness (AI Context Strategist Mode)
	// If the string contains signs of execution failure, we window the 1600-byte allowance
	// around the first major error indicator.
	if IsStackTrace(s) {
		errorIdx := strings.Index(strings.ToLower(s), "error")
		if errorIdx == -1 {
			errorIdx = strings.Index(strings.ToLower(s), "at ")
		}

		// If hit is significant (beyond the first 400 chars), center around it
		if errorIdx > 400 && errorIdx < len(s)-400 {
			windowStart := errorIdx - 400
			windowEnd := errorIdx + 1200 // Allowing total of 1600 bytes context centered on error

			if windowStart < 0 {
				windowStart = 0
			}
			if windowEnd > len(s) {
				windowEnd = len(s)
			}

			errContext := s[windowStart:windowEnd]
			truncatedCount := strings.Count(s, "\n")

			return fmt.Sprintf("```text:error_context\n%s\n```\n\n[... %d LINES / %d CHARACTERS TRUNCATED BY BASTION ORCHESTRATOR - ERROR DETECTED ...]",
				errContext, truncatedCount, len(s)-len(errContext))
		}
	}

	// 🛡️ Standard Expert Split: 800 Start / 800 End (50/50 Strategy)
	startLimit := 800
	endLimit := 800

	if len(s) <= startLimit+endLimit {
		return s
	}

	prefix := s[:startLimit]
	suffix := s[len(s)-endLimit:]

	truncatedCount := strings.Count(s[startLimit:len(s)-endLimit], "\n")
	truncatedChars := len(s) - startLimit - endLimit

	marker := fmt.Sprintf("\n\n[... %d LINES / %d CHARACTERS TRUNCATED BY BASTION ORCHESTRATOR ...]\n\n",
		truncatedCount, truncatedChars)

	return prefix + marker + suffix
}

// IsStackTrace identifies if a string likely contains an execution error/stack trace
func IsStackTrace(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "error") || strings.Contains(s, "at ") || strings.Contains(s, "panic") || strings.Contains(s, "traceback")
}

// LevenshteinDistance calculates the edit distance between two strings.
func LevenshteinDistance(s, t string) int {
	if s == t {
		return 0
	}
	if s == "" {
		return utf8.RuneCountInString(t)
	}
	if t == "" {
		return utf8.RuneCountInString(s)
	}

	sRunes := []rune(s)
	tRunes := []rune(t)
	n := len(sRunes)
	m := len(tRunes)

	// Optimization: swap to ensure n <= m
	if n > m {
		sRunes, tRunes = tRunes, sRunes
		n, m = m, n
	}

	row := make([]int, n+1)
	for i := 0; i <= n; i++ {
		row[i] = i
	}

	for j := 1; j <= m; j++ {
		prev := j
		for i := 1; i <= n; i++ {
			cost := 1
			if sRunes[i-1] == tRunes[j-1] {
				cost = 0
			}
			current := min(row[i]+1, prev+1, row[i-1]+cost)
			row[i-1] = prev
			prev = current
		}
		row[n] = prev
	}

	return row[n]
}
