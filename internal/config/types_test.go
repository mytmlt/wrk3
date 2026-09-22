package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "wrk3.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validBase = `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  run: "echo run"
  stop: "echo stop"
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
`

func TestValidateTable(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{"happy", func(s string) string { return s }, ""},
		{"empty worktreeBase", func(s string) string {
			return strings.Replace(s, "worktreeBase: .worktrees", "worktreeBase: \"\"", 1)
		}, "project.worktreeBase"},
		{"empty source.type", func(s string) string {
			return strings.Replace(s, "type: git", "type: \"\"", 1)
		}, "source.type"},
		{"unknown source.type", func(s string) string {
			return strings.Replace(s, "type: git", "type: svn", 1)
		}, "unknown source type"},
		{"unknown runner.type", func(s string) string {
			return strings.Replace(s, "type: docker", "type: swarm", 1)
		}, "unknown runner type"},
		{"local happy", func(s string) string {
			return strings.Replace(s, "type: docker", "type: local", 1)
		}, ""},
		{"empty composeFiles", func(s string) string {
			return strings.Replace(s, "composeFiles: [docker-compose.yml]", "composeFiles: []", 1)
		}, "composeFiles"},
		{"empty projectPrefix", func(s string) string {
			return strings.Replace(s, "projectPrefix: demo", "projectPrefix: \"\"", 1)
		}, ""},
		{"podman happy", func(s string) string {
			s = strings.Replace(s, "type: docker", "type: podman", 1)
			s = strings.Replace(s, "  docker:\n    composeFiles: [docker-compose.yml]\n    projectPrefix: demo",
				"  podman:\n    composeFiles: [docker-compose.yml]\n    projectPrefix: demo", 1)
			return s
		}, ""},
		{"podman empty composeFiles", func(s string) string {
			s = strings.Replace(s, "type: docker", "type: podman", 1)
			s = strings.Replace(s, "  docker:\n    composeFiles: [docker-compose.yml]\n    projectPrefix: demo",
				"  podman:\n    composeFiles: []\n    projectPrefix: demo", 1)
			return s
		}, "runner.podman.composeFiles"},
		{"podman empty projectPrefix", func(s string) string {
			s = strings.Replace(s, "type: docker", "type: podman", 1)
			s = strings.Replace(s, "  docker:\n    composeFiles: [docker-compose.yml]\n    projectPrefix: demo",
				"  podman:\n    composeFiles: [docker-compose.yml]\n    projectPrefix: \"\"", 1)
			return s
		}, ""},
		{"empty entry.run", func(s string) string {
			return strings.Replace(s, `run: "echo run"`, `run: ""`, 1)
		}, "entry.run"},
		{"empty entry.stop", func(s string) string {
			return strings.Replace(s, `stop: "echo stop"`, `stop: ""`, 1)
		}, "entry.stop"},
		{"ports default", func(s string) string {
			return strings.Replace(s, "ports:\n  base: {app: 8000}\n  ranges:\n    app: [8000, 8099]\n", "", 1)
		}, ""},
		{"port out of range", func(s string) string {
			return strings.Replace(s, "base: {app: 8000}", "base: {app: 99999}", 1)
		}, "ports base"},
		{"legacy step rejected", func(s string) string {
			return s + "  step: 100\n"
		}, "ports.step was removed"},
		{"missing range", func(s string) string {
			return strings.Replace(s, "  ranges:\n    app: [8000, 8099]", "  ranges:\n    web: [3000, 3099]", 1)
		}, "ports ranges"},
		{"base outside range", func(s string) string {
			return strings.Replace(s, "app: [8000, 8099]", "app: [8001, 8099]", 1)
		}, "outside its range"},
		{"bad proxy addr", func(s string) string {
			return s + "proxy:\n  enabled: true\n  addr: \"noport\"\n"
		}, "proxy.addr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, c.mutate(validBase))
			cfg, err := Load(path)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Load = %v, want nil", err)
				}
				if c.name == "ports default" {
					if cfg.Ports.Base["app"] != 8000 {
						t.Errorf("default base app = %v, want 8000", cfg.Ports.Base)
					}
					if cfg.Ports.Ranges["app"] != [2]int{8000, 8099} {
						t.Errorf("default ranges app = %v, want [8000 8099]", cfg.Ports.Ranges["app"])
					}
				}
				if cfg.EffectiveRemote() != "origin" {
					t.Errorf("EffectiveRemote = %q, want origin", cfg.EffectiveRemote())
				}
				return
			}
			if err == nil {
				t.Fatalf("Load = nil, want error containing %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestLoadReload(t *testing.T) {
	full := `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  run: "echo run"
  stop: "echo stop"
  reload: ["echo one", "echo two"]
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
`
	p := writeConfig(t, full)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(cfg.Entry.Reload) != 2 || cfg.Entry.Reload[0] != "echo one" {
		t.Errorf("Reload = %v, want [echo one echo two]", cfg.Entry.Reload)
	}
	// Missing reload stays empty (reload command errors at runtime).
	p2 := writeConfig(t, validBase)
	cfg2, err := Load(p2)
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if len(cfg2.Entry.Reload) != 0 {
		t.Errorf("Reload = %v, want empty", cfg2.Entry.Reload)
	}
}

func TestLoadBadFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("Load(missing) = nil, want error")
	}
	path := writeConfig(t, "::: not yaml :::")
	if _, err := Load(path); err == nil {
		t.Error("Load(bad yaml) = nil, want error")
	}
}

func TestComposeOptionsPodman(t *testing.T) {
	body := `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: podman
  podman:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  run: "echo run"
  stop: "echo stop"
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
`
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	opts := cfg.ComposeOptions("feat-x")
	if opts.ProjectName() != "demo-feat-x" {
		t.Errorf("ProjectName = %q, want demo-feat-x", opts.ProjectName())
	}
	if len(opts.ComposeFiles) != 1 || opts.ComposeFiles[0] != "docker-compose.yml" {
		t.Errorf("ComposeFiles = %v, want [docker-compose.yml]", opts.ComposeFiles)
	}
	if got := cfg.ComposeFiles(); len(got) != 1 || got[0] != "docker-compose.yml" {
		t.Errorf("ComposeFiles() = %v, want [docker-compose.yml]", got)
	}
}

func TestEmptyProjectPrefix(t *testing.T) {
	dockerBase := `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
%sentry:
  run: "echo run"
  stop: "echo stop"
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
`
	cases := []struct {
		name    string
		frag    string
		wantPre string
	}{
		{"explicit empty", "    projectPrefix: \"\"\n", ""},
		{"missing key", "", ""},
		{"whitespace-only", "    projectPrefix: \"   \"\n", ""},
	}
	for _, c := range cases {
		t.Run("docker "+c.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, strings.Replace(dockerBase, "%s", c.frag, 1)))
			if err != nil {
				t.Fatalf("Load = %v, want nil", err)
			}
			if cfg.Runner.Docker.ProjectPrefix != c.wantPre {
				t.Errorf("ProjectPrefix = %q, want %q", cfg.Runner.Docker.ProjectPrefix, c.wantPre)
			}
			if got := cfg.ComposeOptions("feat-x").ProjectName(); got != "feat-x" {
				t.Errorf("ProjectName = %q, want feat-x", got)
			}
		})
	}

	podmanBase := `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: podman
  podman:
    composeFiles: [docker-compose.yml]
%sentry:
  run: "echo run"
  stop: "echo stop"
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
`
	for _, c := range cases {
		t.Run("podman "+c.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, strings.Replace(podmanBase, "%s", c.frag, 1)))
			if err != nil {
				t.Fatalf("Load = %v, want nil", err)
			}
			if cfg.Runner.Podman.ProjectPrefix != c.wantPre {
				t.Errorf("ProjectPrefix = %q, want %q", cfg.Runner.Podman.ProjectPrefix, c.wantPre)
			}
			if got := cfg.ComposeOptions("feat-x").ProjectName(); got != "feat-x" {
				t.Errorf("ProjectName = %q, want feat-x", got)
			}
		})
	}
}

func TestUsesCompose(t *testing.T) {
	if (*Config)(nil).UsesCompose() {
		t.Fatal("nil config UsesCompose = true")
	}
	docker := validBase
	cfg, err := Load(writeConfig(t, docker))
	if err != nil {
		t.Fatalf("Load docker: %v", err)
	}
	if !cfg.UsesCompose() {
		t.Fatal("docker UsesCompose = false")
	}
	localBody := strings.Replace(validBase, "type: docker", "type: local", 1)
	cfg, err = Load(writeConfig(t, localBody))
	if err != nil {
		t.Fatalf("Load local: %v", err)
	}
	if cfg.UsesCompose() {
		t.Fatal("local UsesCompose = true")
	}
	if got := cfg.ComposeFiles(); got != nil {
		t.Errorf("local ComposeFiles() = %v, want nil", got)
	}
}
