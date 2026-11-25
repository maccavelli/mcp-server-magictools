package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshFailureRestoresExactPreviousDefinition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.plist")
	old := []byte("<plist><dict><key>old</key><string>&amp;</string></dict></plist>\n")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return path, true }
	t.Cleanup(func() { serviceDefinitionPathFn = oldPath })
	oldRender := renderDefinitionForRefresh
	renderDefinitionForRefresh = func() (string, error) { return "new-definition\n", nil }
	t.Cleanup(func() { renderDefinitionForRefresh = oldRender })
	oldReload := reloadServiceDefinitionManager
	reloadServiceDefinitionManager = func() error { return errors.New("planted reload failure") }
	t.Cleanup(func() { reloadServiceDefinitionManager = oldReload })
	receipt, err := refreshServiceDefinition()
	if err == nil {
		t.Fatal("reload failure accepted")
	}
	if receipt.Changed {
		t.Fatalf("failed refresh returned success receipt: %#v", receipt)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(old) {
		t.Fatalf("restored bytes = %q, want %q", got, old)
	}
	if _, statErr := os.Stat(path + ".prev"); statErr != nil {
		t.Fatalf("backup not retained: %v", statErr)
	}
}

func TestRestoreServiceDefinitionAfterFailedRefresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.plist")
	old := []byte("exact-old\n")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".prev", old, 0o600); err != nil {
		t.Fatal(err)
	}
	oldReload := reloadServiceDefinitionManager
	reloadServiceDefinitionManager = func() error { return nil }
	t.Cleanup(func() { reloadServiceDefinitionManager = oldReload })
	if err := restoreServiceDefinition(refreshReceipt{Changed: true, Path: path, BackupPath: path + ".prev"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(old) {
		t.Fatalf("restored = %q", got)
	}
	if _, err := os.Stat(path + ".prev"); !os.IsNotExist(err) {
		t.Fatal("backup not cleaned after successful restore")
	}
}
