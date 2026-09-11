package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// FindConfigUpwards walks up from dir (or cwd when dir == "")
// looking for ConfigFileName. It returns the absolute path on success
// or ("", nil) when no config is found.
func FindConfigUpwards(dir string) (string, error) {
	start := dir
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		start = cwd
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve start dir %q: %w", start, err)
	}
	cur := abs
	for {
		candidate := filepath.Join(cur, ConfigFileName)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil
		}
		cur = parent
	}
}

// Resolve implements the resolution order:
// explicit name > $WRK3_PROJECT > current project > cwd scan.
// The returned Project always carries an absolute ConfigPath.
func (s *FileStore) Resolve(explicit string) (*Project, error) {
	if explicit != "" {
		p, err := s.Get(explicit)
		if err != nil {
			return nil, fmt.Errorf("resolve explicit project %q: %w", explicit, err)
		}
		return p, nil
	}
	if v := os.Getenv(envProjectName); v != "" {
		p, err := s.Get(v)
		if err != nil {
			return nil, fmt.Errorf("resolve $%s project %q: %w", envProjectName, v, err)
		}
		return p, nil
	}
	if cur, err := s.Current(); err == nil {
		return cur, nil
	}
	found, err := FindConfigUpwards("")
	if err != nil {
		return nil, err
	}
	if found != "" {
		return &Project{Name: "(cwd)", ConfigPath: found}, nil
	}
	return nil, fmt.Errorf(
		"resolve project: no project found (pass --project <name>, set $%s, use \"project use <name>\", or run inside a directory containing %s)",
		envProjectName, ConfigFileName,
	)
}
