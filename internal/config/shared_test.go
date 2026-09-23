package config

import (
	"strings"
	"testing"
)

const validSharedBlock = `shared:
  docker:
    composeFiles: [docker-compose.dev.yml]
    project: demo-shared
    services:
      db: {ports: ["5432:5432"]}
      rabbitmq: {ports: ["5672:5672", "15672:15672"]}
  worktreeServices: [app]
  env:
    DB_HOST: host.docker.internal
    DB_NAME: superplane_dev_${slug_underscore}
  setup:
    - docker exec ${shared_project}-db-1 createdb -U postgres ${slug_underscore} || true
`

func withShared(base string) string {
	return base + validSharedBlock
}

func TestValidateShared(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{"absent disables", func(s string) string { return s }, ""},
		{"happy", func(s string) string { return withShared(s) }, ""},
		{"env only still requires backend", func(s string) string {
			return s + "shared:\n  env:\n    DB_HOST: x\n"
		}, "shared.docker.composeFiles"},
		{"empty project", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "project: demo-shared", "project: \"\"", 1)
		}, "shared.docker.project"},
		{"empty services", func(s string) string {
			s = withShared(s)
			old := "    services:\n      db: {ports: [\"5432:5432\"]}\n      rabbitmq: {ports: [\"5672:5672\", \"15672:15672\"]}"
			return strings.Replace(s, old, "    services: {}", 1)
		}, "shared.docker.services"},
		{"bad service name", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "      db: {ports:", "      \"not a svc\": {ports:", 1)
		}, "not a valid compose service name"},
		{"empty publish entry", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, `ports: ["5432:5432"]`, `ports: [""]`, 1)
		}, "must not be empty"},
		{"missing worktreeServices", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "  worktreeServices: [app]\n", "", 1)
		}, "shared.worktreeServices"},
		{"overlapping scopes", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "  worktreeServices: [app]", "  worktreeServices: [app, db]", 1)
		}, "must not overlap"},
		{"bad worktree service name", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "  worktreeServices: [app]", "  worktreeServices: [\"nope!\"]", 1)
		}, "not a valid compose service name"},
		{"env key collides with port key", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "    DB_HOST: host.docker.internal", "    APP_PORT: \"1\"", 1)
		}, "collides with managed"},
		{"env key invalid", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "    DB_HOST: host.docker.internal", "    \"9BAD\": x", 1)
		}, "not a valid .env variable name"},
		{"env key reserves WRK3_", func(s string) string {
			s = withShared(s)
			return strings.Replace(s, "    DB_HOST: host.docker.internal", "    WRK3_FOO: x", 1)
		}, "reserved WRK3_ namespace"},
		{"wrong backend block", func(s string) string {
			s = withShared(s)
			return s + "  podman:\n    composeFiles: [x.yml]\n"
		}, "only shared.docker applies"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.mutate(validBase)))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSharedHelpers(t *testing.T) {
	cfg, err := Load(writeConfig(t, withShared(validBase)))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.HasShared() {
		t.Error("HasShared should be true")
	}
	if got := cfg.SharedProject(); got != "demo-shared" {
		t.Errorf("SharedProject = %q", got)
	}
	if got := cfg.SharedServiceNames(); strings.Join(got, ",") != "db,rabbitmq" {
		t.Errorf("SharedServiceNames = %v", got)
	}
	env := cfg.ExpandSharedEnv("feature/foo")
	if env["DB_NAME"] != "superplane_dev_feature_foo" {
		t.Errorf("ExpandSharedEnv DB_NAME = %q", env["DB_NAME"])
	}
	if env["DB_HOST"] != "host.docker.internal" {
		t.Errorf("ExpandSharedEnv DB_HOST = %q", env["DB_HOST"])
	}
	setup := cfg.ExpandSharedSetup("feature/foo")
	if len(setup) != 1 || !strings.Contains(setup[0], "demo-shared-db-1") || !strings.Contains(setup[0], "feature_foo") {
		t.Errorf("ExpandSharedSetup = %v", setup)
	}
	if got := cfg.SharedHostPorts(); len(got) != 3 || got[0] != 5432 || got[1] != 5672 || got[2] != 15672 {
		t.Errorf("SharedHostPorts = %v, want [5432 5672 15672]", got)
	}
	opts := cfg.ComposeOptions("feature-foo")
	if !opts.NoDeps || len(opts.Services) != 1 || opts.Services[0] != "app" {
		t.Errorf("ComposeOptions worktree scope = %+v", opts)
	}
	if len(opts.ExtraFiles) != 0 {
		t.Errorf("ComposeOptions should omit unwritten overlay, got %v", opts.ExtraFiles)
	}
	shared := cfg.SharedRunnerOptions()
	if shared.Slug != "" || len(shared.Services) != 2 {
		t.Errorf("SharedRunnerOptions = %+v", shared)
	}
}

func TestSharedDisabledHelpers(t *testing.T) {
	cfg, err := Load(writeConfig(t, validBase))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HasShared() {
		t.Error("HasShared should be false")
	}
	opts := cfg.ComposeOptions("x")
	if opts.NoDeps || len(opts.Services) != 0 || len(opts.ExtraFiles) != 0 {
		t.Errorf("disabled shared must not scope worktree options: %+v", opts)
	}
}
