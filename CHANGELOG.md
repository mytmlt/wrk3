# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `.env` handling no longer overwrites secrets: `add`/`up`/`down`/`exec`
  upsert only wrk3-managed keys (`<NAME>_PORT`, `BASE_URL`,
  `WEBHOOKS_BASE_URL`, `ALLOWED_WS_ORIGINS`) into an existing `.env`
  (updated in place or appended under a `# Managed by wrk3` marker),
  preserving all other lines. `remove` strips only managed keys instead of
  deleting the file.

### Added

- Main checkout (repo root) is now included implicitly in `up`/`down`
  (bare = all including main), `status`/`ls`, and as an `exec`/`logs`
  target. No state entry; reserved port index `-1` with `.env` written on
  run. `remove` refuses main and `remove --all` covers only managed
  worktrees.
## [0.3.2] - 2026-09-12

### Fixed

- `fetch --mine` and `add --remote --mine` now match branches whose tip
  commit author **or** committer equals git config `user.name`/`user.email`
  (and `--author` matches author/committer substrings). Bot/cursor branches
  you pushed — tip author is the bot, tip committer is you — are now
  included, closer to GitHub's "Yours" (branches you've pushed to).

## [0.3.1] - 2026-09-12

### Fixed

- Asset filename had a wrong `v` prefix (`wrk3_v0.3.0_...`); GoReleaser
  strips it (`wrk3_0.3.0_...`). `wrk3 update` and `scripts/install.sh`
  now build the correct name (tag/URLs keep the `v`).
- `scripts/install.sh` extracted the wrong path after checksum
  verification (renamed package); it now tracks the renamed file.
- Lint: drop deprecated `tar.TypeRegA` in the update extractor.

## [0.3.0] - 2026-09-12

### Added

- `wrk3 update [--version vX.Y.Z] [--check]`: self-update from GitHub
  releases — downloads the matching prebuilt asset, verifies sha256, and
  atomically replaces the running binary. No Go toolchain required.
- Daily update notice: commands print `A new version of wrk3 is
  available: vX.Y.Z (you have vA.B.C). Run "wrk3 update" to update.` to
  stderr when GitHub reports a newer release. Cached for 24h in
  `~/.cache/wrk3/latest-check.json`, 2s timeout, silent offline, skipped
  for `dev` builds / `version` / `completion` / `update` / `--help`;
  opt out with `WRK3_NO_UPDATE_CHECK=1`.

### Changed

- `scripts/install.sh` is now fully Go-free: no `go install` fallback
  (errors with a releases link instead), `X.Y.Z`/`vX.Y.Z` normalization,
  `--bindir`/`BINDIR` support, `--no-verify` flag, `wget` fallback,
  permission/PATH hints, and `wrk3 update` as the update path.
- Install docs (`README.md`, `docs/INSTALL.md`) lead with prebuilt
  binaries; building from source is documented as developers-only.

## [0.2.0] - 2026-09-12

### Added

- `wrk3 add --remote <name> [--mine]`: bulk-create worktrees from a git
  remote (`--remote origin`, `--remote upstream`). Bare `--remote` creates
  every branch on that remote; `--mine` keeps only branches whose tip
  commit author matches git config `user.name`/`user.email`. Already
  registered or already checked-out branches (e.g. `main` at the repo
  root) are skipped with a message. Explicit `add --remote <name> <branch>`
  creates one branch, tracking `<remote>/<branch>` when remote-only.
  The default remote is now honored from `source.git.remote` (else
  `origin`); `fetch` gains the same `--remote` override.
- `Source` interface is remote-aware (`Fetch`/`Refs`/`RefsDetailed`/`Add`
  take a `remote`; empty means `origin`) and `Add` auto-creates tracking
  branches (`--track -b`) for remote-only names. See `docs/PLUGINS.md`.

### Changed

- Agent skills extracted to the standalone `mytmlt/wrk3-skills` repo
  (`skills/` here is now a pointer — see `skills/README.md`).
  `wrk3-setup` now starts with a Phase 0 compatibility triage (new
  `wrk3-compat` skill): the agent analyzes the project's docker compose
  files (`container_name:`, hardcoded vs `${VAR:-default}` host ports,
  bind mounts, `network_mode: host`) and local setup (Makefile / README /
  package.json scripts) and emits a `COMPATIBLE` / `COMPATIBLE WITH CHANGES`
  / `INCOMPATIBLE` verdict with file:line evidence before authoring any
  `wrk3.yaml`.

## [0.1.0] - 2026-09-12

### Removed

- **Breaking:** project registry (`project add|list|use|remove|show`,
  `~/.config/wrk3/projects.yaml`, `--project`, `$WRK3_PROJECT`, `--config`,
  `status --all`, `PROJECT` column). Config resolution is now compose-style:
  `-f/--file <path>` > upward scan for `wrk3.yaml`/`wrk3.yml`.
- **Breaking:** `up --all` / `down --all` flags — bare `up`/`down` now apply
  to all worktrees (pass names to filter).

### Added

- `wrk3 completion <bash|zsh|fish|powershell>` (fixes `make completion`).
- Dynamic completion: `add` completes remote branches; `up`/`down`/`logs`/
  `exec`/`remove` complete existing worktrees (branch or slug). Offline-safe
  (no fetch on TAB).
- `remove [branch...] | --all` (variadic); bare `add` opens the interactive
  branch picker (same as `--select`).
- `add --local [branch...]`: adopt branches that already have local git
  worktrees (port assign + `.env` + state) without fetching from origin.
  Bare adopts every unregistered local worktree (main checkout, bare repos
  and detached HEADs skipped); explicit names adopt only those.
- `internal/config/discover.go` with `wrk3.yaml`/`wrk3.yml` upward discovery.
- Auto-registered project list: `wrk3 add` records the repo (root dir
  basename) in `~/.config/wrk3/projects.yaml`; `wrk3 project ls` lists
  projects (`NAME/CONFIG/WORKTREES`); `wrk3 ls` lists local worktrees
  (`WORKTREE/BRANCH/STATUS/APP`); `wrk3 ls --project <name>` lists a
  registered project from anywhere.
- `up` preflight check: compose files setting `container_name:` fail fast
  naming the offending file/services (it bypasses `-p <prefix>-<slug>`
  isolation and collides across worktrees) instead of failing minutes
  into setup with a daemon conflict.

## [0.0.1] - 2026-09-11

### Added

- Initial `wrk3` release: run multiple branches of the same repo in
  parallel as git worktrees, each with isolated ports and a compose project.
- `project add|list|use|remove|show` registry (`~/.config/wrk3/projects.yaml`).
- `fetch`, `add` (incl. `--select`), `up` (parallel setup+run), `down`,
  `logs [-f]`, `status [--all]`, `exec <branch> -- <cmd>`, `remove [--force]`.
- `wrk3.yaml` config with `git` source and `docker` runner,
  deterministic `base + index*step` port allocation, and per-worktree `.env`.
- Stub `portainer`/`nomad` runners returning `not implemented`.
- `fetch --mine` / `--author <name-or-email>`: list only your remote
  branches, filtered by tip-commit author (`--mine` matches git config
  `user.name`/`user.email`; `--author` is a case-insensitive substring
  match, repeatable and comma-split; combined they intersect).
- System-wide installation: `Makefile` (`make install`), `scripts/install.sh`
  (release assets with checksum verification, `go install` fallback), and
  GoReleaser config producing `linux`/`darwin`/`windows` archives.
- Version reporting: `wrk3 version` and `wrk3 --version`, stamped via
  `-ldflags` at build/release time.
- Open-source baseline: `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, user docs under `docs/`, CI + release workflows, issue/PR
  templates, Dependabot, and the `wrk3-setup` agent skill in `skills/`.
- Plugin authoring guide: `docs/PLUGINS.md` documenting the `Source` and
  `Runner` interfaces, registry, config/`cmd` wiring, and verification.

### Changed

- Generic ports: `ports.base` defaults to `{app: 8000}` with
  `app` required; extra names map to generic `<NAME>_PORT` `.env` vars
  (plus `BASE_URL` family from `app`).
- `wrk3.yaml` no longer takes `project.repo`: the repo root is the
  directory containing the config (the config always lives in the
  project root). `project.worktreeBase` resolves against it.
- `status` shows `PROJECT/WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT`.

### Fixed

- CI pipeline: bump `actions/checkout` v4→v7, `actions/setup-go` v5→v7,
  `golangci/golangci-lint-action` v6→v9 (v6 ships golangci-lint built
  with go1.24 and cannot parse go1.26), and
  `goreleaser/goreleaser-action` v6→v7; fix `errcheck` failures on
  `fmt.Fprint*` to CLI output; make `ports`/`project`/`source` tests
  path-portable and skip linux-only `docker` integration tests on
  Windows so `windows-latest` goes green.

[Unreleased]: https://github.com/mytmlt/wrk3/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/mytmlt/wrk3/releases/tag/v0.3.2
[0.3.1]: https://github.com/mytmlt/wrk3/releases/tag/v0.3.1
[0.3.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.3.0
[0.2.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.2.0
[0.1.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.1.0
[0.0.1]: https://github.com/mytmlt/wrk3/releases/tag/v0.0.1
