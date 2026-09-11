package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Sentinel errors for registry operations.
var (
	ErrNotFound  = errors.New("project not found")
	ErrNoCurrent = errors.New("no current project set")
)

// ConfigFileName is the file FindConfigUpwards looks for.
const ConfigFileName = "wrk3.yaml"

// Env vars controlling path resolution.
const (
	envConfigHome  = "XDG_CONFIG_HOME"
	envOverride    = "WRK3_CONFIG_HOME"
	envProjectName = "WRK3_PROJECT"
)

// fileEntry is one registry entry on disk.
type fileEntry struct {
	ConfigPath string    `yaml:"configPath"`
	AddedAt    time.Time `yaml:"addedAt"`
}

// fileData is the on-disk YAML shape.
type fileData struct {
	Projects       map[string]fileEntry `yaml:"projects"`
	CurrentProject string               `yaml:"currentProject"`
}

// FileStore is a YAML-backed Store at ~/.config/wrk3/projects.yaml
// (XDG-aware). All paths stored are absolute, so commands work from any
// cwd. Writes are atomic (temp file + rename) under a best-effort
// lock file.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// Option customizes a FileStore.
type Option func(*FileStore)

// WithPath overrides the registry file path (used in tests).
func WithPath(path string) Option {
	return func(s *FileStore) { s.path = path }
}

// DefaultPath resolves the registry file path.
// Precedence: $WRK3_CONFIG_HOME/wrk3/projects.yaml >
// $XDG_CONFIG_HOME/wrk3/projects.yaml > ~/.config/wrk3/projects.yaml.
func DefaultPath() (string, error) {
	if v := os.Getenv(envOverride); v != "" {
		return filepath.Join(v, "wrk3", "projects.yaml"), nil
	}
	if v := os.Getenv(envConfigHome); v != "" {
		return filepath.Join(v, "wrk3", "projects.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve config home: %w", err)
	}
	return filepath.Join(home, ".config", "wrk3", "projects.yaml"), nil
}

// NewFileStore creates a FileStore at DefaultPath (or WithPath override).
func NewFileStore(opts ...Option) (*FileStore, error) {
	def, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	s := &FileStore{path: def}
	for _, o := range opts {
		o(s)
	}
	if s.path == "" {
		return nil, fmt.Errorf("project store path must not be empty")
	}
	return s, nil
}

// Path returns the registry file path.
func (s *FileStore) Path() string { return s.path }

// lockPath returns the sidecar lock file path.
func (s *FileStore) lockPath() string { return s.path + ".lock" }

// load reads the registry file; missing file yields an empty registry.
func (s *FileStore) load() (*fileData, error) {
	d := &fileData{Projects: map[string]fileEntry{}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return nil, fmt.Errorf("read registry %q: %w", s.path, err)
	}
	if len(raw) == 0 {
		return d, nil
	}
	if err := yaml.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("parse registry %q: %w", s.path, err)
	}
	if d.Projects == nil {
		d.Projects = map[string]fileEntry{}
	}
	return d, nil
}

// save writes data atomically: temp file in the same dir + rename.
func (s *FileStore) save(d *fileData) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create registry dir %q: %w", dir, err)
	}
	raw, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "projects-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("create temp registry file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp registry file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp registry file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("chmod temp registry file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace registry %q: %w", s.path, err)
	}
	return nil
}

// acquireLock creates the lock file exclusively, retrying briefly.
// Best-effort: callers must call release() (removes the file).
func (s *FileStore) acquireLock() (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return nil, fmt.Errorf("create registry dir for lock: %w", err)
	}
	const tries = 200
	const delay = 10 * time.Millisecond
	for i := 0; i < tries; i++ {
		f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(s.lockPath()) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire registry lock: %w", err)
		}
		time.Sleep(delay)
	}
	return nil, fmt.Errorf("acquire registry lock %q: timed out waiting for lock", s.lockPath())
}

// mutate loads, applies fn, and saves under lock + mutex.
func (s *FileStore) mutate(fn func(d *fileData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.acquireLock()
	if err != nil {
		return err
	}
	defer release()
	d, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(d); err != nil {
		return err
	}
	if err := s.save(d); err != nil {
		return err
	}
	return nil
}

// absolutize resolves p to an absolute path (relative to cwd at call
// time) and errors on empty input.
func absolutize(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("config path must not be empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", p, err)
	}
	return filepath.Clean(abs), nil
}

// Add inserts or updates name -> absolute configPath.
// Relative paths resolve against the cwd at add time.
func (s *FileStore) Add(name, configPath string) error {
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	abs, err := absolutize(configPath)
	if err != nil {
		return err
	}
	return s.mutate(func(d *fileData) error {
		e := d.Projects[name]
		if e.ConfigPath == "" {
			e.AddedAt = time.Now().UTC()
		}
		e.ConfigPath = abs
		d.Projects[name] = e
		return nil
	})
}

// Get returns the project by name.
func (s *FileStore) Get(name string) (*Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	e, ok := d.Projects[name]
	if !ok {
		return nil, fmt.Errorf("get project %q: %w", name, ErrNotFound)
	}
	return &Project{Name: name, ConfigPath: e.ConfigPath, AddedAt: e.AddedAt}, nil
}

// List returns all projects sorted by name.
func (s *FileStore) List() ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Project, 0, len(d.Projects))
	for name, e := range d.Projects {
		out = append(out, Project{Name: name, ConfigPath: e.ConfigPath, AddedAt: e.AddedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Remove deletes name; clears current if it pointed at name.
func (s *FileStore) Remove(name string) error {
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	return s.mutate(func(d *fileData) error {
		if _, ok := d.Projects[name]; !ok {
			return fmt.Errorf("remove project %q: %w", name, ErrNotFound)
		}
		delete(d.Projects, name)
		if d.CurrentProject == name {
			d.CurrentProject = ""
		}
		return nil
	})
}

// SetCurrent marks name as the current project.
func (s *FileStore) SetCurrent(name string) error {
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	return s.mutate(func(d *fileData) error {
		if _, ok := d.Projects[name]; !ok {
			return fmt.Errorf("use project %q: %w", name, ErrNotFound)
		}
		d.CurrentProject = name
		return nil
	})
}

// CurrentName returns the current project name ("", nil when unset).
func (s *FileStore) CurrentName() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return "", err
	}
	return d.CurrentProject, nil
}

// Current returns the current project or ErrNoCurrent when unset.
func (s *FileStore) Current() (*Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.load()
	if err != nil {
		return nil, err
	}
	if d.CurrentProject == "" {
		return nil, ErrNoCurrent
	}
	e, ok := d.Projects[d.CurrentProject]
	if !ok {
		return nil, fmt.Errorf("current project %q: %w", d.CurrentProject, ErrNotFound)
	}
	return &Project{Name: d.CurrentProject, ConfigPath: e.ConfigPath, AddedAt: e.AddedAt}, nil
}

// Compile-time check: FileStore implements Store.
var _ Store = (*FileStore)(nil)
