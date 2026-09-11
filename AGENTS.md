# AGENTS.md — wrk3

`wrk3` runs multiple branches of the same repo in parallel as git worktrees,
each isolated with its own ports and container project. Single static Go
binary (`Go ≥ 1.26`); runtime deps are `git` and `docker` (docker only for
the `docker` runner).

## Essential commands

```bash
go build ./... && go vet ./... && go test ./... -count=1   # full gate
make test        # same via Makefile
make build       # version-stamped ./bin/wrk3
./bin/wrk3 --help && ./bin/wrk3 version
```

Run the full gate before finishing any code change. `internal/runner` tests
take ~10s (they exercise real `docker`); everything else is fast and
hermetic (temp git repos, temp dirs — no network).

## Layout and architecture (binding)

- `main.go` → `cmd/` (thin cobra commands) → `internal/*` **interfaces only**.
  `cmd` must never import a concrete `git`/`docker` implementation.
- `internal/project/` — `Project{Name, ConfigPath, AddedAt}` + `FileStore`
  registry at `~/.config/wrk3/projects.yaml` (XDG-aware; absolute paths).
- `internal/source/` — `Source` iface + `registry.go` + `git.go`.
- `internal/runner/` — `Runner` iface + `registry.go` + `docker.go`
  (`portainer`/`nomad` are intentional `not implemented` stubs).
- `internal/ports/` — `allocated = base + index*step` allocator, `.env`
  writer, `<worktreeBase>/.wrk3-state.json` state file.
- `internal/config/` — `wrk3.yaml` load + validation (`docs/CONFIGURATION.md`
  is the field reference).

## Rules

- New `Source`/`Runner` backends **must** register in their `registry.go`;
  unknown `source.type`/`runner.type` values error listing available options.
- State file paths stay absolute (cwd-independence). `status` shows
  `?`/`stale` when a worktree dir is missing — never fail hard there.
- Branch slugs: `feature/foo` → `feature-foo`, max 50 chars
  (`internal/source/slug.go`).
- Entry strings (`setup`/`run`/`stop`/`logs`) execute verbatim via `sh -c`
  with `cwd=worktree`, `env=allocated ports` — never hardcode repo-specific
  commands (e.g. `make test`) in Go code.
- Keep functions small, wrap errors with context
  (`fmt.Errorf("...: %w", err)`), no secrets in logs or commits.

## Config and resolution

- Example: `wrk3.yaml.example`. Resolution order for every command:
  `--config` > `--project` > `$WRK3_PROJECT` > current project > cwd scan
  for `wrk3.yaml`.
- For config-authoring questions (new stack, broken config, port mapping),
  follow `skills/wrk3-setup/SKILL.md`.

## Verifying behavior changes

Unit tests live next to the code (`*_test.go`). For CLI-level verification,
use an isolated registry and a temp git repo — never touch the user's real
registry or checkouts:

```bash
export WRK3_CONFIG_HOME="$(mktemp -d)"
go run . project add demo --config ./wrk3.yaml.example
go run . status
```

`add`/`up` create real worktrees/containers — only run them against throwaway
repos, and `down` + `remove` afterwards to clean up.

## Don'ts

- Don't commit personal configs (`wrk3.*.local.yaml`), `.wrk3-state.json`, `.worktrees/`, `.env` files,
  binaries, or anything with absolute personal paths / secrets.
- Don't reformat unrelated files (`gofmt -l` has two pre-existing findings
  in `internal/config/types.go` and `internal/runner/docker_test.go` — leave
  them).
- User-facing change? Update `README.md` / `docs/USAGE.md` /
  `docs/CONFIGURATION.md` plus a `CHANGELOG.md` `[Unreleased]` entry.
- Security issues: see `SECURITY.md` — never file public issues for them.
