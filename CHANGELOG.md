# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- The repo-root main checkout now serves exactly the `ports.base`
  allocation (reserved index `0`, e.g. `app=8000` with defaults) instead
  of `base` minus one step (e.g. `app=7900`). Managed worktrees allocate
  from `max(index)+1` floored at `1`, so the first `add` takes index `1`
  (e.g. `app=8100`) and existing managed indexes are never renumbered.
  State files predating this fix that still hold a managed index `0`
  allocation now surface a `main worktree ports collide ...` error
  instead of a silent port conflict — `remove` + re-`add` that worktree
  to reallocate it at a fresh index.

## [0.9.0] - 2026-09-16

### Added

- `wrk3 pull [branch...]` (bare = all worktrees including main, in
  parallel like `up`/`down`): runs `git pull` inside each target worktree
  (`--rebase` / `--ff-only` map to the git flags, mutually exclusive).
  Branch names and slugs resolve interchangeably including main, stale
  entries error out, and dirty worktrees surface the git error. Backed by
  a new `Source.Pull(worktreePath, PullOptions)` method (`git -C
  <worktree> pull`).
- `wrk3 reload [branch...]` (bare = all worktrees including main, in
  parallel like `up`/`down`): runs the new `entry.reload` ordered list via
  `sh -c` with `cwd=worktree` and `env=allocated ports`, marking targets
  `setting up` → `running`/`failed`. Errors when `entry.reload` is empty.
  The dashboard runs the same op with `l` on the selected/cursor
  worktrees (optimistic `setting up` flip, `reload` in the shortcut bar).
- Dashboard worktree links + `db` shorthand: the worktree table grows a
  `URL` column (gateway `<slug>.<domain>` URL when `proxy.enabled`, else
  `localhost:<appPort>`; compacted on narrow terminals), `o` opens the
  cursor worktree URL in a browser (full URL is logged too), `O` copies
  that URL to the OS clipboard (`pbcopy` on macOS, `wl-copy`/`xclip`/`xsel`
  on Linux, `clip` on Windows), the meta line
  shows gateway state, and `wrk3 db` aliases `wrk3 dashboard`.
- Always-on proxy: `add` (all modes) and the dashboard (refresh plus TUI
  `up`/`add`) now ensure the gateway when `proxy.enabled`, best-effort
  like `up` (spawn errors warn, commands never fail).
- Standing agent autonomy: new `skills/wrk3-dev-flow/SKILL.md` checklist
  (isolate → gate → autonomous commit/push/PR → `gh pr checks --watch`;
  never merge), `AGENTS.md` pre-authorizes the flow without asking, and
  project `opencode.json` gains `skills.paths` plus `permission.bash`
  for the workflow (`wrk3`/`make`/rebase/`gh pr create|edit`/topic-branch
  pushes allowed; `main`/tag pushes and any merge denied). Also adds the
  `wrk3-test` slash command (`go vet ./cmd/ && go test ./cmd/ -run
  '<pattern>' -count=1`, empty args = full gate).
- `wrk3 checkout <branch|slug>` (`co`/`switch` aliases) jumps to a
  worktree folder: with shell integration (`eval "$(wrk3 shell-init
  bash)"` once in `~/.bashrc` — `zsh`/`fish`/`powershell` also covered
  via `wrk3 shell-init <shell>`) it cds the calling shell through a
  worktrunk-style directive file (`$WRK3_DIRECTIVE_CD_FILE`, raw path,
  never parsed as shell); without it, it prints the absolute path so
  `cd "$(wrk3 checkout x)"` always works. Branch names and slugs resolve
  interchangeably including the implicit main checkout, stale/missing
  directories error out, and `--print` forces the script-safe
  print-never-cd form. `shell-init` is excluded from the update nag
  (it is eval'd at every shell start).
- Dogfood config: committed `wrk3.yaml` for the wrk3 repo itself so
  agents (and humans) run `wrk3 fetch` / `wrk3 add` here per the
  `AGENTS.md` isolate step. No docker stack exists, so `up`/`down` are
  not meaningful (Go gate instead) and `status` shows `unknown`.
- `AGENTS.md` gains the binding agent development workflow (isolate on
  branch + worktree, test with the full gate, autonomous
  commit/push/PR/checks-watching with `gh pr checks --watch`; human
  merges when green).

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

### Changed

- `.env` management no longer sets hardcoded `BASE_URL`,
  `WEBHOOKS_BASE_URL`, or `ALLOWED_WS_ORIGINS`: managed keys are exactly
  one `<NAME>_PORT` per `ports.base` entry (plus append-only `APP_URL`
  when `proxy.enabled`). Existing URL values copy verbatim from the
  repo-root `.env` seed into fresh worktrees and are preserved on
  `remove` instead of being stripped.

### Fixed

- `wrk3 update` no longer 404s: the release download URLs dropped the
  stray `/repos/` segment (an `api.github.com` path shape that does not
  exist on the `github.com` download host).

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

[0.9.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.9.0
[0.8.0]: https://github.com/mytmlt/wrk3/releases/tag/v0.8.0
