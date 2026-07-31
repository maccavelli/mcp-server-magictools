package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to a temporary file in targetPath's directory,
// calls fsync, and atomically renames it to targetPath.
func atomicWriteFile(targetPath string, data []byte) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file in %s: %w", dir, err)
	}
	tmpName := tmpFile.Name()

	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName) //nolint:errcheck // cleanup temp file on error
		}
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		_ = tmpFile.Close() //nolint:errcheck // cleanup file handle on error
		return fmt.Errorf("failed to set 0600 permissions on %s: %w", tmpName, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close() //nolint:errcheck // cleanup file handle on error
		return fmt.Errorf("failed to write data to %s: %w", tmpName, err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close() //nolint:errcheck // cleanup file handle on error
		return fmt.Errorf("failed to sync %s: %w", tmpName, err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to replace %s with %s: %w", targetPath, tmpName, err)
	}

	tmpName = "" // Prevent defer cleanup after successful rename

	// Sync parent directory where supported
	if parentFile, err := os.Open(filepath.Clean(dir)); err == nil {
		_ = parentFile.Sync()  //nolint:errcheck // best-effort directory sync
		_ = parentFile.Close() //nolint:errcheck // best-effort directory handle close
	}

	return nil
}
