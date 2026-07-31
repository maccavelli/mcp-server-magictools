package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStore_SetAndRemove(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `# Header Comment
configuration:
  logLevel: INFO
  intelligence:
    provider: gemini
    model: gemini-2.5-flash
    thinking_provider: claude
    thinking_api_key: sk-old-key
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(configPath)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(paths)

	// Patch: Change logLevel to DEBUG and remove thinking_api_key
	patch := ConfigurationPatch{
		Runtime: RuntimeConfigPatch{
			LogLevel: Set("DEBUG"),
		},
		Thinking: ThinkingTierPatch{
			ThinkingAPIKey: Remove[string](),
		},
	}

	res, err := store.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("store.Apply failed: %v", err)
	}

	if !res.Changed {
		t.Fatal("expected res.Changed == true")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	strData := string(data)
	if !strings.Contains(strData, "# Header Comment") {
		t.Errorf("expected header comment to be preserved:\n%s", strData)
	}
	if !strings.Contains(strData, "logLevel: DEBUG") {
		t.Errorf("expected logLevel: DEBUG:\n%s", strData)
	}
	if strings.Contains(strData, "thinking_api_key:") {
		t.Errorf("expected thinking_api_key to be removed:\n%s", strData)
	}
}

func TestStore_NoOpDoesNotTouchFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `configuration:
  logLevel: WARN
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(configPath)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(paths)
	res, err := store.Apply(context.Background(), ConfigurationPatch{})
	if err != nil {
		t.Fatalf("store.Apply failed: %v", err)
	}

	if res.Changed {
		t.Fatal("expected res.Changed == false for empty patch")
	}
}

func TestStore_ConcurrentWriters(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `configuration:
  logLevel: INFO
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(configPath)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(paths)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := ConfigurationPatch{
				Fast: FastTierPatch{
					RetryCount: Set(idx + 1),
				},
			}
			_, _ = store.Apply(context.Background(), p)
		}(i)
	}
	wg.Wait()

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("file was corrupted during concurrent apply")
	}
}
