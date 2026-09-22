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

const validNoneBase = `project:
  worktreeBase: .worktrees
source:
  type: git
runner:
  type: none
entry:
  run: ""
  stop: ""
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
		{"none runner happy", func(s string) string {
			return validNoneBase
		}, ""},
		{"none runner empty run ok", func(s string) string {
			return strings.Replace(validNoneBase, `run: ""`, `run: ""`, 1)
		}, ""},
		{"none runner empty worktreeBase", func(s string) string {
			return strings.Replace(validNoneBase, "worktreeBase: .worktrees", "worktreeBase: \"\"", 1)
		}, "project.worktreeBase"},
		{"empty runner.type defaults to none", func(s string) string {
			return strings.Replace(validNoneBase, "type: none", "type: \"\"", 1)
		}, ""},
		{"none runner step still rejected", func(s string) string {
			return validNoneBase + "ports:\n  step: 100\n"
		}, "ports.step was removed"},
		{"privileged proxy port", func(s string) string {
			return s + "proxy:\n  enabled: true\n  addr: \"127.0.0.1:80\"\n"
		}, "requires root"},
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

func TestValidateURLs(t *testing.T) {
	base := `project:
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
urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8000, 8099]
`
	urlBlock := `urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8000, 8099]`
	cases := []struct {
		name    string
		wantErr string
		urlYAML string
	}{
		{"happy", "", urlBlock},
		{"empty var", "urls[0].var must not be empty", `urls:
  - var: ""
    base: http://localhost:8000
    range: [8000, 8099]`},
		{"duplicate var", "is duplicated", `urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8000, 8099]
  - var: APP_URL
    base: http://localhost:9000
    range: [9000, 9099]`},
		{"var collides with managed port", "collides with managed port key", `urls:
  - var: APP_PORT
    base: http://localhost:8000
    range: [8000, 8099]`},
		{"invalid var", "not a valid .env variable name", `urls:
  - var: 123URL
    base: http://localhost:8000
    range: [8000, 8099]`},
		{"no explicit port", "must have an explicit port", `urls:
  - var: APP_URL
    base: http://localhost
    range: [8000, 8099]`},
		{"inverted range", "min must be <= max", `urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8099, 8000]`},
		{"base port outside range", "outside its range", `urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8001, 8099]`},
		{"invalid range values", "ports must be 1", `urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [0, 8099]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := strings.Replace(base, urlBlock, c.urlYAML, 1)
			path := writeConfig(t, body)
			cfg, err := Load(path)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("Load = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Load = nil, want error containing %q", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), c.wantErr)
			}
			if cfg != nil {
				t.Logf("config loaded but expected error; cfg.Urls = %+v", cfg.Urls)
			}
		})
	}
}

func TestURLSpecs_Conversion(t *testing.T) {
	body := `project:
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
urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8000, 8099]
  - var: BASE_URL
    base: https://api.example.com:443/api/v1
    range: [443, 500]
`
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	specs := cfg.URLSpecs()
	if len(specs) != 2 {
		t.Fatalf("URLSpecs len = %d, want 2", len(specs))
	}
	if specs[0].Var != "APP_URL" || specs[1].Var != "BASE_URL" {
		t.Errorf("specs vars = %q, %q", specs[0].Var, specs[1].Var)
	}
	if specs[0].BaseURL.String() != "http://localhost:8000" {
		t.Errorf("APP_URL BaseURL = %q", specs[0].BaseURL)
	}
	if specs[0].Range != [2]int{8000, 8099} {
		t.Errorf("APP_URL Range = %v", specs[0].Range)
	}
}

func TestURLBasePorts(t *testing.T) {
	body := `project:
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
urls:
  - var: APP_URL
    base: http://localhost:8000
    range: [8000, 8099]
  - var: BASE_URL
    base: https://api.example.com:443/path
    range: [400, 500]
`
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	bports := cfg.URLBasePorts()
	if bports["APP_URL"] != 8000 {
		t.Errorf("APP_URL base port = %d, want 8000", bports["APP_URL"])
	}
	if bports["BASE_URL"] != 443 {
		t.Errorf("BASE_URL base port = %d, want 443", bports["BASE_URL"])
	}
}
