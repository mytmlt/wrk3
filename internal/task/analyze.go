package task

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse unmarshals canonical Task YAML.
func Parse(raw []byte) (*Task, error) {
	var t Task
	if err := yaml.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse task: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// AnalyzeOptions controls how a directory is turned into a Task.
type AnalyzeOptions struct {
	// From is "compose", "host", or empty (auto: compose files, else host).
	From string
	// Files are compose or host plan paths, relative to Dir unless absolute.
	// Empty Files discovers compose files when From is compose/auto.
	Files []string
}

// Analyze builds a Task from dir without modifying any source file.
func Analyze(dir string, opts AnalyzeOptions) (*Task, []Note, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	from := strings.ToLower(strings.TrimSpace(opts.From))
	switch from {
	case "", "auto":
		if len(opts.Files) > 0 {
			return analyzeNamed(dir, opts.Files)
		}
		files, err := DiscoverComposeFiles(dir)
		if err != nil {
			return nil, nil, err
		}
		if len(files) > 0 {
			return AnalyzeCompose(dir, files)
		}
		return nil, nil, fmt.Errorf("no compose file found in %q (looked for %s); pass files or --from host", dir, strings.Join(composeFilenames, ", "))
	case "compose":
		files := opts.Files
		if len(files) == 0 {
			var err error
			files, err = DiscoverComposeFiles(dir)
			if err != nil {
				return nil, nil, err
			}
		}
		if len(files) == 0 {
			return nil, nil, fmt.Errorf("no compose file found in %q (looked for %s)", dir, strings.Join(composeFilenames, ", "))
		}
		return AnalyzeCompose(dir, files)
	case "host":
		if len(opts.Files) == 0 {
			return nil, nil, fmt.Errorf("host plan requires at least one file")
		}
		return AnalyzeHost(dir, opts.Files)
	default:
		return nil, nil, fmt.Errorf("unknown source %q (available sources: [compose host])", opts.From)
	}
}

func analyzeNamed(dir string, files []string) (*Task, []Note, error) {
	// Prefer compose parse; fall back to host if compose has no services.
	t, notes, err := AnalyzeCompose(dir, files)
	if err == nil && t != nil && len(t.Services) > 0 {
		return t, notes, nil
	}
	return AnalyzeHost(dir, files)
}

// AnalyzeCompose reads compose files (later files overlay earlier) into a Task.
func AnalyzeCompose(dir string, files []string) (*Task, []Note, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("analyze compose: no files")
	}
	paths := resolveFiles(dir, files)
	doc, err := loadCompose(paths)
	if err != nil {
		return nil, nil, err
	}
	svcs, notes, err := servicesFromCompose(doc)
	if err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(doc.Name)
	if name == "" {
		name = dirName(dir)
	}
	t := &Task{
		Name:     name,
		Source:   Source{Kind: SourceCompose, Files: append([]string(nil), files...)},
		Services: svcs,
		Networks: networksFromCompose(doc),
		Volumes:  volumesFromCompose(doc, svcs),
	}
	if err := t.Validate(); err != nil {
		return nil, notes, err
	}
	return t, notes, nil
}

// AnalyzeHost reads host plan YAML files into a Task.
func AnalyzeHost(dir string, files []string) (*Task, []Note, error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("analyze host: no files")
	}
	paths := resolveFiles(dir, files)
	svcs, name, err := loadHost(paths)
	if err != nil {
		return nil, nil, err
	}
	if name == "" {
		name = dirName(dir)
	}
	t := &Task{
		Name:     name,
		Source:   Source{Kind: SourceHost, Files: append([]string(nil), files...)},
		Services: svcs,
		Volumes:  volumesFromCompose(rawCompose{}, svcs),
	}
	if err := t.Validate(); err != nil {
		return nil, nil, err
	}
	return t, nil, nil
}
