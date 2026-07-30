// Package dag provides functionality for the dag subsystem.
package dag

import (
	"crypto/sha256"
	"fmt"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// GetEdgeScore returns a normalized [0,1] confidence score for a tool-to-tool
// transition, reading from existing synergy tracking data written by
// generate_audit_report's learning intercept.
//
// The hash format matches generate_audit_report.go line 229:
//
//	SHA256(fromURN + "->" + toURN)
//
// Returns 0.0 if no transition data exists for this pair.
// Uses Laplace smoothing: success / (success + penalty + 1) to avoid division
// by zero and to provide damped confidence for low-sample transitions.
func GetEdgeScore(store *db.Store, fromURN, toURN string) float64 {
	if store == nil {
		return 0.0
	}

	hash := transitionHash(fromURN, toURN)
	successes, penalties := store.GetSynergy(hash)

	total := successes + penalties
	if total == 0 {
		return 0.0 // No data — neutral score
	}

	// Laplace-smoothed probability
	return float64(successes) / float64(total+1)
}

// transitionHash computes the SHA256 hex digest for a tool-to-tool edge,
// matching the exact format used in generate_audit_report.go.
func transitionHash(fromURN, toURN string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fromURN+"->"+toURN)))
}
