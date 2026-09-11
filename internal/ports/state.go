package ports

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StatePath joins worktreeBase with the state file name.
func StatePath(worktreeBase string) string {
	return filepath.Join(worktreeBase, StateFileName)
}

// validateRecords enforces absolute AbsPath entries.
func validateRecords(records []WorktreeRecord) error {
	for i, r := range records {
		if r.AbsPath == "" {
			return fmt.Errorf("state record %d (%q): absPath must not be empty", i, r.Branch)
		}
		if !filepath.IsAbs(r.AbsPath) {
			return fmt.Errorf("state record %d (%q): absPath must be absolute: %q", i, r.Branch, r.AbsPath)
		}
	}
	return nil
}

// Load reads the state file at path. A missing file yields an empty
// slice. Stored AbsPath entries must be absolute.
func Load(path string) ([]WorktreeRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorktreeRecord{}, nil
		}
		return nil, fmt.Errorf("read state %q: %w", path, err)
	}
	if len(raw) == 0 {
		return []WorktreeRecord{}, nil
	}
	var records []WorktreeRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("parse state %q: %w", path, err)
	}
	if records == nil {
		return []WorktreeRecord{}, nil
	}
	if err := validateRecords(records); err != nil {
		return nil, err
	}
	return records, nil
}

// Save writes records as indented JSON to path, enforcing absolute
// AbsPath entries. The write is atomic (temp file + rename).
func Save(path string, records []WorktreeRecord) error {
	if err := validateRecords(records); err != nil {
		return err
	}
	if records == nil {
		records = []WorktreeRecord{}
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir %q: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wrk3-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("chmod temp state file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state %q: %w", path, err)
	}
	return nil
}
