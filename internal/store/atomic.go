package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes payload to path through a temp file in the same
// directory followed by a rename, so an interrupted run never leaves a
// truncated document behind.
func writeFileAtomic(path string, payload []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".write-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temp file %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tempName, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("persist %s: %w", path, err)
	}
	return nil
}
