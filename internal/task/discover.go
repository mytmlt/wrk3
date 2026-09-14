package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Default compose filenames, in Compose-spec preference order.
var composeFilenames = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

var composeOverrideFilenames = []string{
	"compose.override.yaml",
	"compose.override.yml",
	"docker-compose.override.yaml",
	"docker-compose.override.yml",
}

// DiscoverComposeFiles returns compose files in dir (base + override
// when present). Empty means none found — not an error.
func DiscoverComposeFiles(dir string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	var found []string
	for _, name := range composeFilenames {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat compose file %q: %w", p, err)
		}
		if st.IsDir() {
			continue
		}
		found = append(found, name)
		break
	}
	if len(found) == 0 {
		return nil, nil
	}
	for _, name := range composeOverrideFilenames {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat compose override %q: %w", p, err)
		}
		if st.IsDir() {
			continue
		}
		found = append(found, name)
		break
	}
	return found, nil
}

func resolveFiles(dir string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if filepath.IsAbs(f) {
			out = append(out, f)
			continue
		}
		out = append(out, filepath.Join(dir, f))
	}
	return out
}

func dirName(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "wrk3"
	}
	base := filepath.Base(abs)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return "wrk3"
	}
	return base
}
