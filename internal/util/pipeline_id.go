package util

import (
	"crypto/rand"
	"fmt"
)

// GeneratePipelineSessionID creates the server-issued capability id for a
// pipeline session: 128 random bits from crypto/rand, hex, prefixed "ep-"
// (0005's D2.4 I-a, adopted by 0013). The existing GenerateSessionID (64 bits)
// keeps its other callers; a pipeline continuation id must be unguessable.
func GeneratePipelineSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read never fails on supported platforms; a failure here
		// is a broken OS entropy source, not a recoverable condition.
		panic(fmt.Sprintf("util: crypto/rand failed: %v", err))
	}
	return "ep-" + fmt.Sprintf("%x", b)
}
