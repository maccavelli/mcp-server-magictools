// Package hfsc implements the orchestrator-side High-Fidelity Smart Chunking
// protocol. It forms the Tier-2 transport receiver, streaming massive base64
// payload chunks linearly directly onto the host Bastion SSD with zero RAM buffering.
package hfsc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// StreamState holds the in-flight state for an extreme-scale streamed artifact.
type StreamState struct {
	mu             sync.Mutex
	SHA256         string // expected hash of the decoded payload (received via FINALIZE)
	TotalChunks    int    // expected chunks (received via FINALIZE)
	Received       int    // chunks received
	Filename       string // original filename
	ProjectID      string
	Model          string
	ServerName     string
	CreatedAt      time.Time
	artifactPath   string
	tempFile       *os.File
	hasher         hash.Hash
	completionChan chan struct{} // signaled when FINALIZE completes
	fault          error         // unrecoverable write faults
}

// Registry manages concurrent HFSC disk-streaming sessions.
type Registry struct {
	sessions    sync.Map // map[string]*StreamState
	artifactDir string
}

// NewRegistry creates an HFSC registry that streams artifacts into cacheDir/artifacts.
func NewRegistry(cacheDir string) *Registry {
	dir := filepath.Join(cacheDir, "artifacts")
	return &Registry{
		artifactDir: dir,
	}
}

// Register creates a new HFSC Tier-2 streaming session and returns a completion channel.
func (r *Registry) Register(sessionID, filename, projectID, model, serverName string) (chan struct{}, error) {
	if _, loaded := r.sessions.Load(sessionID); loaded {
		return nil, fmt.Errorf("hfsc: session %s already registered", sessionID)
	}

	if err := os.MkdirAll(r.artifactDir, 0o750); err != nil { //nolint:gosec // G301: artifact dir under user cache
		return nil, fmt.Errorf("hfsc: artifact dir prep failed: %w", err)
	}

	safeFilename := fmt.Sprintf("%s_%s.part", sessionID, filepath.Base(filename))
	artifactPath := filepath.Join(r.artifactDir, safeFilename)

	tmpFile, err := os.Create(filepath.Clean(artifactPath)) //nolint:gosec // path constructed from controlled artifact directory
	if err != nil {
		return nil, fmt.Errorf("hfsc: failed to create stream buffer file: %w", err)
	}

	doneCh := make(chan struct{})
	s := &StreamState{
		Filename:       filename,
		ProjectID:      projectID,
		Model:          model,
		ServerName:     serverName,
		CreatedAt:      time.Now(),
		artifactPath:   artifactPath,
		tempFile:       tmpFile,
		hasher:         sha256.New(),
		completionChan: doneCh,
	}

	r.sessions.Store(sessionID, s)
	telemetry.OptMetrics.HFSCActiveStreams.Add(1)

	slog.Info("hfsc: tier-2 extreme session registered on disk",
		"session", sessionID,
		"filename", filename,
		"server", serverName,
	)
	return doneCh, nil
}

// AccumulateChunk writes a base64 text chunk immediately into the native file buffer.
func (r *Registry) AccumulateChunk(sessionID string, index int, chunkText string) error {
	raw, ok := r.sessions.Load(sessionID)
	if !ok {
		return fmt.Errorf("hfsc: chunk arrived for unknown or finalized session %s", sessionID)
	}
	s, ok := raw.(*StreamState)
	if !ok {
		return fmt.Errorf("hfsc: invalid session state for %s", sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.fault != nil {
		return fmt.Errorf("hfsc: stream write fault previously occurred: %w", s.fault)
	}

	b, err := base64.StdEncoding.DecodeString(chunkText)
	if err != nil {
		s.fault = fmt.Errorf("base64 decode error at chunk %d: %w", index, err)
		return s.fault
	}

	if _, err := s.tempFile.Write(b); err != nil {
		s.fault = fmt.Errorf("disk write panic at chunk %d: %w", index, err)
		return s.fault
	}

	s.hasher.Write(b)
	s.Received++

	// Rolling log output based on arbitrary chunks to show life
	if s.Received%500 == 0 {
		slog.Debug("hfsc: milestone", "session", sessionID, "chunks_written", s.Received)
	}

	return nil
}

// FinalizeStream is invoked by the explicit protocol HFSC_FINALIZE log message.
// It seals the disk stream, executes the rolling hashing comparison, renames the
// .part file into the final artifact identity, and signals the orchestrator block.
func (r *Registry) FinalizeStream(sessionID string, totalChunks int, expectedHash string) (artifactPath string, err error) {
	raw, ok := r.sessions.LoadAndDelete(sessionID)
	if !ok {
		return "", fmt.Errorf("hfsc: stream requested finalize but not found in active registry")
	}
	telemetry.OptMetrics.HFSCActiveStreams.Add(-1)
	s, ok := raw.(*StreamState)
	if !ok {
		return "", fmt.Errorf("hfsc: invalid session state for %s", sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Safe shutdown of stream
	if closeErr := s.tempFile.Close(); closeErr != nil {
		removeArtifactBestEffort(s.artifactPath)
		return "", fmt.Errorf("hfsc: close stream buffer: %w", closeErr)
	}

	if s.fault != nil {
		removeArtifactBestEffort(s.artifactPath)
		return "", fmt.Errorf("stream failed during continuous transfer: %w", s.fault)
	}

	if s.Received != totalChunks {
		removeArtifactBestEffort(s.artifactPath)
		return "", fmt.Errorf("hfsc: chunk drop mismatch! expected %d but received %d", totalChunks, s.Received)
	}

	actualHash := hex.EncodeToString(s.hasher.Sum(nil))
	if actualHash != expectedHash {
		removeArtifactBestEffort(s.artifactPath)
		return "", fmt.Errorf("hfsc: catastrophic integrity checksum mismatch! expected %s, got %s", expectedHash, actualHash)
	}

	// Rename out of .part into production identity
	finalSafeFilename := fmt.Sprintf("%s_%s", sessionID, filepath.Base(s.Filename))
	finalPath := filepath.Join(r.artifactDir, finalSafeFilename)

	if err := os.Rename(s.artifactPath, finalPath); err != nil {
		return "", fmt.Errorf("hfsc: failed to bind finalized artifact %s: %w", finalPath, err)
	}

	telemetry.OptMetrics.HFSCReassemblySuccesses.Add(1)
	slog.Info("hfsc: tier-2 extreme stream successfully secured on SSD",
		"session", sessionID,
		"sha256", actualHash,
		"chunks", totalChunks,
	)

	// Unleash orchestrator proxy trap
	if s.completionChan != nil {
		close(s.completionChan)
	}

	return finalPath, nil
}

// ArtifactDir returns the directory where HFSC artifacts are stored.
func (r *Registry) ArtifactDir() string {
	return r.artifactDir
}

func removeArtifactBestEffort(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Debug("hfsc: artifact remove failed", "path", path, "error", err)
	}
}

// ParseStreamWire detects both Tier-2 chunks and finalization signals natively.
func ParseStreamWire(wire string) (isFin bool, sessionID string, index int, chunkOrHash string, err error) {
	parts := strings.SplitN(wire, "|", 5)
	if len(parts) != 5 {
		return false, "", 0, "", fmt.Errorf("invalid v2 wire length")
	}

	op := parts[0]
	if op != "HFSC_STREAM" && op != "HFSC_FINALIZE" {
		return false, "", 0, "", fmt.Errorf("unsupported root protocol %s", op)
	}
	if parts[1] != "v2" {
		return false, "", 0, "", fmt.Errorf("unsupported protocol ver %s", parts[1])
	}

	isFin = (op == "HFSC_FINALIZE")
	sessionID = parts[2]
	index, err = strconv.Atoi(parts[3])
	if err != nil {
		return isFin, sessionID, 0, "", fmt.Errorf("invalid cursor index %w", err)
	}

	chunkOrHash = parts[4]
	return isFin, sessionID, index, chunkOrHash, nil
}

// StartCleanupSweep scavenges orphaned .part files across crashes.
func (r *Registry) StartCleanupSweep(ctx context.Context, ttl time.Duration) {
	r.cleanStaleArtifacts()
	go func() {
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.cleanStaleArtifacts()
			}
		}
	}()
}

// cleanStaleArtifacts sweeps .part orphans older than 10 mins.
func (r *Registry) cleanStaleArtifacts() {
	entries, err := os.ReadDir(r.artifactDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-10 * time.Minute)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		// Always nuke stale .part ghost streams
		if strings.HasSuffix(e.Name(), ".part") && info.ModTime().Before(cutoff) {
			path := filepath.Join(r.artifactDir, e.Name())
			if err := os.Remove(path); err == nil {
				slog.Info("hfsc: swept unresolvable ghost stream", "path", path, "age", time.Since(info.ModTime()))
			}
		}
	}
}
