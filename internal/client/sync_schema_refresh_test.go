package client

import (
	"os"
	"reflect"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSyncSchemaRefresh_RetypingChangeDoesNotSkip holds 0005 D3.3.
// A stored schema left by the old sanitiser (an untyped property rewritten to
// type: object) must not match the hash of the same downstream tool after the
// D3.1 sanitiser, so shouldSkipUnchangedSync is false and the saved schema is
// the new form. A tool the new sanitiser leaves unchanged keeps its hash and
// is skipped.
func TestSyncSchemaRefresh_RetypingChangeDoesNotSkip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "magictools-schema-refresh-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })
	store, err := db.NewStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	m := NewWarmRegistry(tempDir, store, &config.Config{})
	sc := config.ServerConfig{Name: "downstream"}

	raw := &mcp.Tool{
		Name:        "loose",
		Description: "an untyped property",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"foo": map[string]any{"description": "bar"},
			},
		},
	}
	// The old sanitiser's output for that property: type object, and then
	// properties: {} because the new type is object and properties was absent.
	oldForm := &mcp.Tool{
		Name:        raw.Name,
		Description: raw.Description,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"foo": map[string]any{
					"description": "bar",
					"type":        "object",
					"properties":  map[string]any{},
				},
			},
		},
	}

	rec, schemaMap := m.buildToolRecord(sc, raw)
	if schemaMap == nil {
		t.Fatal("saved schema is nil")
	}
	props, _ := schemaMap["properties"].(map[string]any)
	foo, _ := props["foo"].(map[string]any)
	if _, ok := foo["type"]; ok {
		t.Fatalf("saved schema still retypes the untyped property: %#v", foo)
	}
	oldHash := m.hashSchema(oldForm)
	if rec.SchemaHash == oldHash {
		t.Fatal("new sanitiser hash equals the old-form hash; a sync would skip")
	}

	oldBatch := []*db.ToolRecord{{URN: rec.URN, SchemaHash: oldHash}}
	store.SaveServerSyncHash(sc.Name, compositeSyncHash(oldBatch))
	if err := store.SaveTool(&db.ToolRecord{URN: rec.URN, Name: rec.Name, Server: sc.Name}); err != nil {
		t.Fatal(err)
	}
	newBatch := []*db.ToolRecord{rec}
	if m.shouldSkipUnchangedSync(sc.Name, compositeSyncHash(newBatch), newBatch) {
		t.Fatal("shouldSkipUnchangedSync true for a schema the sanitiser changed")
	}
	if err := store.BatchSaveTools([]*db.ToolRecord{rec}, map[string]map[string]any{rec.SchemaHash: schemaMap}); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetSchema(rec.SchemaHash)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, schemaMap) {
		t.Fatalf("stored schema = %#v, want %#v", saved, schemaMap)
	}

	stable := &mcp.Tool{
		Name:        "typed",
		Description: "already typed",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string"},
			},
		},
	}
	stableRec, _ := m.buildToolRecord(sc, stable)
	same := m.hashSchema(stable)
	if stableRec.SchemaHash != same {
		t.Fatalf("unchanged tool hash = %s, want %s", stableRec.SchemaHash, same)
	}
	stableBatch := []*db.ToolRecord{stableRec}
	store.SaveServerSyncHash("stable", compositeSyncHash(stableBatch))
	if err := store.SaveTool(&db.ToolRecord{URN: "stable:typed", Name: "typed", Server: "stable"}); err != nil {
		t.Fatal(err)
	}
	if !m.shouldSkipUnchangedSync("stable", compositeSyncHash(stableBatch), stableBatch) {
		t.Fatal("shouldSkipUnchangedSync false for a schema the sanitiser leaves unchanged")
	}
}
