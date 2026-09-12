package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Candidate file names for auto-discovery (in preference order per directory).
// wrk3.yaml wins over wrk3.yml when both exist side by side.
var candidateNames = []string{"wrk3.yaml", "wrk3.yml"}

// DiscoverFile resolves the config file to use.
//
//   - When explicit != "": absolutize it and return it (caller loads it,
//     so a missing file surfaces as a load error, like docker compose -f).
//   - Otherwise walk up from dir (or cwd when dir == "") looking for
//     wrk3.yaml / wrk3.yml, nearest directory wins.
//
// Returns ("", nil) when nothing is found and no explicit path was given.
func DiscoverFile(explicit, dir string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve --file %q: %w", explicit, err)
		}
		return filepath.Clean(abs), nil
	}
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
		for _, name := range candidateNames {
			candidate := filepath.Join(cur, name)
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", nil
		}
		cur = parent
	}
}
