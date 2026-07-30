package db

import (
	"context"
	"os"
	"testing"
)

func TestStoreExhaustive(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "db-exhaustive-test")
	defer os.RemoveAll(tmpDir)
	s, err := NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	s.SaveTool(&ToolRecord{URN: "test:foo", Name: "foo", Category: "system"})
	s.GetTool("test:foo")
	s.SearchTools(context.Background(), "foo", "system", "", 0.0, 0.5, DomainSystem, false)
	s.GetCategories()
	s.AnalyzeIntent(context.Background(), "foo")
	s.SaveSchema("hash1", map[string]any{"type": "string"})
	s.GetSchema("hash1")
	s.SaveRawResource("id", []byte("xxx"))
	s.GetRawResource("id")
	s.UpdateToolUsage("test:foo")
	s.PurgeServerTools("test")
	s.GetCategories()
	s.Close()
}
