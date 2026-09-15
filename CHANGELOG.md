# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `wrk3 add <new-branch>` now offers to create the branch when the name
  matches no local or remote branch: it refreshes the remote once (stale
  cache guard, offline-safe), then prompts `Create new branch from
  <remote>/<default>? [y/N]` and creates the worktree with
  `git worktree add -b` from the remote default branch (falls back to
  `HEAD` with a warning when the default is unknown). `--create` skips
  the prompt, `--no-create` fails fast instead (for scripts/CI). Only
  explicit branch names ever create; `--select`, `--local`, and bulk
  `--remote`/`--mine`/`--myprs` never do.
- `README.md` now leads with the `dashboard`: screenshot
  (`docs/dashboard.png`), a `Dashboard (start here)` section first
  (panes, key table, flags), dashboard-first `Features` list and
  Quickstart.
- Docs now explain worktree reconcile + docker runtime sync: `USAGE.md`
  gains `Worktree reconcile` / `Runtime sync + display` subsections
  (adoption order, grid-validity + collision scan, hard-error collisions,
  offline-safe list, parallel probes, persist/display matrices, docker
  primary + label-based fallbacks); `README.md` summarizes with a link,
  `CONFIGURATION.md` points at it.

### Security

- `scripts/install.sh` now fails closed when checksum verification is
  enabled (default): missing `checksums.txt`, missing asset entry, failed
  download, or no `sha256sum`/`shasum` tool aborts the install instead of
  warning and continuing. Use `--no-verify` / `WRK3_VERIFY=0` to explicitly
  opt into an unverified install.

## [0.8.0] - 2026-09-15

Initial public release.

### Added

- Parallel git worktrees: `wrk3 add <branch...>` (explicit names,
  interactive picker, `--select`, `--remote <name> [--mine]`,
  `--local` adoption of existing checkouts, orphan adoption) with
  `feature/foo` → `feature-foo` slugs (max 50 chars), tracking-branch
  creation for remote-only branches, and `<worktreeBase>/.wrk3-state.json`
  state (absolute paths, `?`/`stale` display for missing dirs, self-healing
  reconcile + runtime sync on reads).
- `docker` runner (`compose -p <prefix>-<slug>`, `cwd=worktree`,
  `env=allocated ports`): `up` (setup entries → compose up → run entry,
  in parallel with `container_name:` preflight fail-fast), `down`
  (runs `entry.stop`, then compose down), `exec`, `logs` (runs
  `entry.logs` when set, else compose logs). `portainer`/`nomad` are
  intentional `not implemented` stubs (see `docs/PLUGINS.md`).
- Deterministic port allocation (`allocated = base + index*step`,
  default `{app: 8000}` / step `100`, main checkout at index `-1`) with
  append-only managed `.env` sections (`<NAME>_PORT`, `BASE_URL` family,
  `APP_URL` when proxied) — existing values and secrets are never
  overwritten; `remove` strips only managed keys.
- Local gateway (`proxy.*`, opt-in stdlib reverse proxy):
  `http://<slug>.localhost:<port>` → each worktree's `app` port,
  `up` auto-starts it, `wrk3 proxy up|down|status|open|hosts-sync`
  manage it, `status` grows a `URL` column.
- BubbleTea `dashboard` TUI (worktree + branch tables, `up`/`down`/`add`/
  `remove`, `--myprs`/`--mine`/`--author` filters, `--poll`, multi-project).
- `--myprs` on `fetch`/`add`/`dashboard`: branches with an open PR
  involving you (`gh`-based, GitHub only, no stored tokens).
- `wrk3 skill` bundled agent guide (onboarding-first discovery → compat
  triage → author + validate `wrk3.yaml`); extended skills in
  `mytmlt/wrk3-skills`. Plugin guide in `docs/PLUGINS.md`.
- Distribution: `scripts/install.sh` (prebuilt assets + sha256, user-local
  `~/.local/bin` default), `wrk3 update [--version] [--check]` self-update
  with daily cached notice, shell completions
  (`bash`/`zsh`/`fish`/`powershell`), version-stamped builds.
- CI: `build + vet + test (-short)` matrix (`ubuntu`/`macos`/`windows`,
  Go 1.26), docker integration job, `Security` workflow (`govulncheck`,
  `gitleaks`, `dependency-review`), `CodeQL`, `golangci-lint` with `gosec`.

### Fixed

- `down` runs `entry.stop` before compose down (warns, never blocks
  teardown); `logs` prefers `entry.logs`; CLI honors `cmd.Context()`
  cancellation and `cmd` IO streams; `compose down` env matches `up`
  (ports + `APP_URL`); every backend builds via the `runner` registry.
- `ls`/`status` never hang on a slow docker daemon (own process group,
  whole group killed on timeout).

[0.8.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.8.0
