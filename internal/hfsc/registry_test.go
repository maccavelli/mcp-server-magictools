package hfsc

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistry_RegisterAndStream(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry(tmpDir)

	if reg.ArtifactDir() != filepath.Join(tmpDir, "artifacts") {
		t.Errorf("unexpected artifact dir")
	}

	sessionID := "sess-123"
	filename := "test.txt"

	_, err := reg.Register(sessionID, filename, "proj1", "mod1", "srv1")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Register again should fail
	if _, err := reg.Register(sessionID, filename, "proj1", "mod1", "srv1"); err == nil {
		t.Errorf("expected error for duplicate registration")
	}

	data := []byte("hello world data")
	chunkText := base64.StdEncoding.EncodeToString(data)

	err = reg.AccumulateChunk(sessionID, 0, chunkText)
	if err != nil {
		t.Fatalf("AccumulateChunk failed: %v", err)
	}

	// Unknown session
	if err := reg.AccumulateChunk("unknown", 0, chunkText); err == nil {
		t.Errorf("expected error for unknown session")
	}

	// Bad base64
	if err := reg.AccumulateChunk(sessionID, 1, "invalid_base64!"); err == nil {
		t.Errorf("expected error for bad base64")
	}

	// Because of bad base64, stream should now be in fault state
	if err := reg.AccumulateChunk(sessionID, 2, chunkText); err == nil {
		t.Errorf("expected error for faulted stream")
	}

	hasher := sha256.New()
	hasher.Write(data)
	hashStr := hex.EncodeToString(hasher.Sum(nil))

	// Finalize should fail because of fault
	_, err = reg.FinalizeStream(sessionID, 1, hashStr)
	if err == nil {
		t.Errorf("expected error finalizing faulted stream")
	}
}

func TestRegistry_SuccessfulStream(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry(tmpDir)

	sessionID := "sess-success"
	filename := "success.txt"

	doneCh, err := reg.Register(sessionID, filename, "proj1", "mod1", "srv1")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	data := []byte("successful data stream")
	chunkText := base64.StdEncoding.EncodeToString(data)

	err = reg.AccumulateChunk(sessionID, 0, chunkText)
	if err != nil {
		t.Fatalf("AccumulateChunk failed: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(data)
	hashStr := hex.EncodeToString(hasher.Sum(nil))

	artifactPath, err := reg.FinalizeStream(sessionID, 1, hashStr)
	if err != nil {
		t.Fatalf("FinalizeStream failed: %v", err)
	}

	if artifactPath == "" {
		t.Errorf("artifact path empty")
	}

	// Check channel closed
	select {
	case <-doneCh:
	default:
		t.Errorf("expected done channel to be closed")
	}

	// Verify file
	savedData, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read finalized file failed: %v", err)
	}
	if string(savedData) != string(data) {
		t.Errorf("data mismatch")
	}

	// Finalize unknown session
	if _, err := reg.FinalizeStream("unknown", 1, hashStr); err == nil {
		t.Errorf("expected error for unknown session")
	}
}

func TestRegistry_FinalizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry(tmpDir)

	sessionID := "sess-mismatch"
	filename := "mismatch.txt"

	_, _ = reg.Register(sessionID, filename, "proj1", "mod1", "srv1")
	data := []byte("successful data stream")
	chunkText := base64.StdEncoding.EncodeToString(data)
	_ = reg.AccumulateChunk(sessionID, 0, chunkText)

	hasher := sha256.New()
	hasher.Write(data)
	hashStr := hex.EncodeToString(hasher.Sum(nil))

	// Chunk count mismatch
	_, err := reg.FinalizeStream(sessionID, 2, hashStr)
	if err == nil {
		t.Errorf("expected error for chunk count mismatch")
	}

	// Redo for hash mismatch
	sessionID2 := "sess-mismatch2"
	_, _ = reg.Register(sessionID2, filename, "proj1", "mod1", "srv1")
	_ = reg.AccumulateChunk(sessionID2, 0, chunkText)
	_, err = reg.FinalizeStream(sessionID2, 1, "badhash")
	if err == nil {
		t.Errorf("expected error for hash mismatch")
	}
}

func TestParseStreamWire(t *testing.T) {
	wire := "HFSC_STREAM|v2|sess-123|0|base64data"
	isFin, sessionID, index, chunk, err := ParseStreamWire(wire)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if isFin || sessionID != "sess-123" || index != 0 || chunk != "base64data" {
		t.Errorf("mismatch ParseStreamWire")
	}

	wireFin := "HFSC_FINALIZE|v2|sess-123|1|hash"
	isFin, sessionID, index, chunk, err = ParseStreamWire(wireFin)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !isFin || sessionID != "sess-123" || index != 1 || chunk != "hash" {
		t.Errorf("mismatch ParseStreamWire fin")
	}

	// Error cases
	if _, _, _, _, err := ParseStreamWire("invalid|len"); err == nil {
		t.Errorf("expected err for length")
	}
	if _, _, _, _, err := ParseStreamWire("INVALID_OP|v2|sess|0|chunk"); err == nil {
		t.Errorf("expected err for op")
	}
	if _, _, _, _, err := ParseStreamWire("HFSC_STREAM|v1|sess|0|chunk"); err == nil {
		t.Errorf("expected err for version")
	}
	if _, _, _, _, err := ParseStreamWire("HFSC_STREAM|v2|sess|invalid|chunk"); err == nil {
		t.Errorf("expected err for index")
	}
}

func TestRegistry_StartCleanupSweep(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry(tmpDir)
	_ = os.MkdirAll(reg.ArtifactDir(), 0755)

	// Create old .part file
	oldPath := filepath.Join(reg.ArtifactDir(), "old.part")
	os.WriteFile(oldPath, []byte("data"), 0644)
	oldTime := time.Now().Add(-11 * time.Minute)
	os.Chtimes(oldPath, oldTime, oldTime)

	// Create new .part file
	newPath := filepath.Join(reg.ArtifactDir(), "new.part")
	os.WriteFile(newPath, []byte("data"), 0644)

	ctx := t.Context()

	reg.StartCleanupSweep(ctx, 100*time.Millisecond)

	time.Sleep(200 * time.Millisecond)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expected old.part to be deleted")
	}

	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("expected new.part to still exist")
	}
}
