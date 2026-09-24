# AGENTS.md — wrk3

`wrk3` runs multiple branches of the same repo in parallel as git worktrees,
each isolated with its own ports and container project. Single static Go
binary (`Go ≥ 1.26`); runtime deps are `git` and a container engine
(`docker` for the `docker` runner, `podman` for the `podman` runner).

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

## Layout and architecture

- `main.go` → `cmd/` (thin cobra commands) → `internal/*` **interfaces only**
  (see `docs/PLUGINS.md` for the one compose-options exception in `cmd/common.go`).
- `internal/config/` — `wrk3.yaml`/`wrk3.yml` load + validation + upward
  discovery (`discover.go`; `docs/CONFIGURATION.md` is the field reference).
- `internal/source/` — `Source` iface + `registry.go` + `git.go`.
- `internal/runner/` — `Runner` iface + `registry.go` + `docker.go` + `podman.go`
  (`portainer`/`nomad` are intentional `not implemented` stubs).
- `internal/ports/` — range allocator (`base` + per-service `ranges`,
  lowest free with gap reuse + OS bind probe), `.env` writer,
  `<worktreeBase>/.wrk3-state.json` state file.
- `internal/forge/` — `gh`-based PR filtering (`--myprs`).
- `internal/proxy/` — stdlib local gateway (`<slug>.<domain>` → app port).
- `internal/project/` — `~/.config/wrk3/projects.yaml` registry.
- `cmd/dashboard*.go` — bubbletea TUI.
- `skills/` — vendored agent skills (canonical source, supersedes the
  standalone `mytmlt/wrk3-skills` repo). `skills/wrk3-setup/` covers
  onboarding: compatibility triage → author + validate `wrk3.yaml`.

## Rules

- Docs + skills stay in sync with behavior — no exceptions. Every change
  to CLI behavior, config shape (`ports`/`urls`/`proxy`/`health`/
  `entry`/`runner`/`source`), dashboard UX, port allocation, `.env`
  management, or setup flow **must** update `docs/` (`USAGE.md` /
  `CONFIGURATION.md`), `skills/wrk3-setup/SKILL.md` (+ `examples/` when the
  template or verdict changes), and a `CHANGELOG.md` `[Unreleased]` entry
  in the same PR. Bump the skill `wrk3-version` frontmatter and the
  `skills/README.md` version table when the tested CLI release moves.
  A PR that changes behavior without touching docs + skills + changelog
  is incomplete.
- New `Source`/`Runner` backends **must** register in their `registry.go`;
  unknown `source.type`/`runner.type` values error listing available options.
- State file paths stay absolute (cwd-independence). `status` shows
  `?`/`stale` when a worktree dir is missing — never fail hard there.
- Branch slugs: `feature/foo` → `feature-foo`, max 50 chars
  (`internal/source/slug.go`).
- Entry strings (`setup`/`run`/`stop`/`logs`/`reload`) execute verbatim via `sh -c`
  with `cwd=worktree`, `env=allocated ports` — never hardcode repo-specific
  commands (e.g. `make test`) in Go code.
- Keep functions small, wrap errors with context
  (`fmt.Errorf("...: %w", err)`), no secrets in logs or commits.

## Config and resolution

- Example: `wrk3.yaml.example`. Resolution order for every command:
  `-f/--file <path>` > upward scan from cwd for `wrk3.yaml`, then `wrk3.yml`
  (nearest directory wins). No registry, no env var — like `docker compose`.
- For config-authoring questions (new stack, broken config, port mapping),
  start from `wrk3.yaml.example` and `docs/CONFIGURATION.md`, plus
  `skills/wrk3-setup/SKILL.md` (+ `skills/wrk3-setup/examples/`) for the
  agent onboarding flow.

## Verifying behavior changes

Unit tests live next to the code (`*_test.go`). For CLI-level verification,
use a temp git repo — never touch the user's real checkouts:

```bash
go run . -f ./wrk3.yaml.example status
```

`add`/`up` create real worktrees/containers — only run them against throwaway
repos, and `down` + `remove` afterwards to clean up.

## Don'ts

- Don't commit personal configs (`wrk3.*.local.yaml`), `.wrk3-state.json`, `.worktrees/`, `.env` files,
  binaries, or anything with absolute personal paths / secrets.
- User-facing change? Update `README.md` / `docs/USAGE.md` /
  `docs/CONFIGURATION.md` **plus** `skills/wrk3-setup/SKILL.md`
  (+ `examples/` + `skills/README.md` version table when the skill changes)
  plus a `CHANGELOG.md` `[Unreleased]` entry — all in the same PR (see
  Rules: docs + skills + changelog stay in sync, no exceptions).
- Security issues: see `SECURITY.md` — never file public issues for them.
