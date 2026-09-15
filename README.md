# wrk3

[![CI](https://github.com/mytmlt/wrk3/actions/workflows/ci.yml/badge.svg)](https://github.com/mytmlt/wrk3/actions/workflows/ci.yml)
[![Release](https://github.com/mytmlt/wrk3/actions/workflows/release.yml/badge.svg)](https://github.com/mytmlt/wrk3/actions/workflows/release.yml)
[![Go](https://img.shields.io/badge/go-1.26-blue)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Run **multiple branches of the same repo in parallel** as git worktrees, each
isolated with its own ports and container project — from any directory.

`wrk3` was built for stacks where one checkout = one full environment
(e.g. a `docker compose` dev stack): point it at a repo, `add` two branches,
`up`, and get two running copies on distinct ports and distinct compose
projects. `status` shows every worktree, branch, and port at a glance.

## Features

- **Parallel worktrees** — `git worktree add/remove/list` behind a `Source`
  interface (`git` ships; the shape reserves future backends).
- **Isolated runners** — `docker compose -p <prefix>-<slug>` per worktree
  behind a `Runner` interface (`docker` ships; `portainer`/`nomad` stubs
  return `not implemented`).
- **Deterministic ports** — `allocated = base + index * step` per port name,
  ensured in each worktree's `.env` (missing managed keys appended under a
  wrk3 section; your existing lines and secrets are never touched, and new
  worktrees inherit non-managed keys from the repo root); `status` reads
  them back from the state file (`?`/`stale` when the directory is missing).
- **Compose-style config** — wrk3 finds `wrk3.yaml`/`wrk3.yml`
  walking up from cwd (`-f/--file` to override), so it works from any
  subdirectory. `add` auto-registers the repo for `project ls` /
  `ls --project`.
- **Shell completion** — `wrk3 completion <bash|zsh|fish|powershell>` plus
  dynamic branch/worktree/project completion for `add`/`up`/`down`/`logs`/`exec`/`remove`/`ls --project`.
- **Single static binary** — Go, no runtime deps besides `git` and `docker`.
 - **Interactive dashboard** — `wrk3 dashboard` polls worktrees, remote
  branches, and ports across the current repo and registered projects
  (`tab` switches), with `up`/`down`/`add`/`remove` from the keyboard
   (`x` removes, `X` force-removes like `remove --force`).
  The CLI keeps working alongside it.

## Install

No Go toolchain needed — `git` and `docker` at runtime
(docker only for the `docker` runner). Full details:
[docs/INSTALL.md](docs/INSTALL.md).

```bash
# Option 1 — release assets (linux/darwin/windows, checksums verified, no sudo: installs to ~/.local/bin)
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash

# Option 2 — download the asset manually from GitHub releases
# (see docs/INSTALL.md for the verify + install steps)

# Option 3 — from source (developers only, requires Go ≥ 1.26)
git clone https://github.com/mytmlt/wrk3.git && cd wrk3
make install            # installs to ~/.local/bin, no sudo (ensure it is on PATH)
# machine-wide instead: sudo make install PREFIX=/usr/local
wrk3 version
```

`wrk3` notifies you when a new release exists
(`WRK3_NO_UPDATE_CHECK=1` to silence) and updates in place:

```bash
wrk3 update --check
wrk3 update
```

Shell completions: `make completion` then source
`completions/wrk3.<bash|zsh|fish|powershell>`.

## Quickstart (5 minutes)

```bash
# 1. Describe your repo (see wrk3.yaml.example + docs/CONFIGURATION.md)
cp wrk3.yaml.example wrk3.yaml  # local use; keep gitignored in app repos
$EDITOR wrk3.yaml  # config lives in the repo root; commit only templates

# 2. Fetch remote branches, create two isolated worktrees (run inside the repo)
wrk3 fetch
wrk3 fetch --mine                    # only your branches (tip or last 100 branch-exclusive commits match git config user)
wrk3 fetch --author alice            # substring match on author/committer name/email (tip or history)
wrk3 fetch --myprs                   # branches with an open PR involving you (GitHub remotes only, via gh)
wrk3 add feature-a feature-b
# or pick interactively: wrk3 add
# or kick off something new (prompts to create from origin/<default>): wrk3 add feat/new-thing
# or bulk-create from a remote: wrk3 add --remote upstream --mine

# 3. Boot both stacks in parallel (setup entries, compose up, run entry)
wrk3 up

# 4. Inspect, tail, run commands
wrk3 status
wrk3 dashboard                  # or drive it all from the TUI
wrk3 ls --project myapp   # same worktrees from any dir (after add registers it)
wrk3 project ls           # all registered projects
wrk3 logs feature-a -f
wrk3 exec feature-a -- make test

# 5. Tear down
wrk3 down
wrk3 remove --all
```

## Configuration

```yaml
project:
  worktreeBase: .worktrees        # relative => resolved against repo root (config dir)
source:
  type: git
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]
    projectPrefix: demo
entry:
  setup: ["docker compose up --wait --build"]
  run: "docker compose logs -f"
  stop: "docker compose down"
  logs: "docker compose logs -f"
ports:
  base: {app: 8000}
  step: 100
```

Full field reference, port table, `.env` mapping, and multi-project patterns:
[docs/CONFIGURATION.md](docs/CONFIGURATION.md). Command reference:
[docs/USAGE.md](docs/USAGE.md).

Project goal: turn **any codebase** into a `wrk3.yaml` that runs the app
the way its developers run it locally — see [ROADMAP.md](ROADMAP.md)
for where the `docker` / `portainer` / `nomad` / bare-machine runners stand.

> **Agents / automation:** run `wrk3 skill` to print the bundled setup
> guide (local-setup discovery + compat triage + `wrk3.yaml` template +
> validation) to stdout —
> no config needed. The [`wrk3-compat`](https://github.com/mytmlt/wrk3-skills/tree/main/skills/wrk3-compat)
> and [`wrk3-setup`](https://github.com/mytmlt/wrk3-skills/tree/main/skills/wrk3-setup)
> skills (standalone [`mytmlt/wrk3-skills`](https://github.com/mytmlt/wrk3-skills) repo —
> `git clone https://github.com/mytmlt/wrk3-skills.git`) analyze a project's compose/local setup
> for wrk3 compatibility and author + validate a `wrk3.yaml` for any repo.

## How it works

1. `wrk3 add <branch>` → `git worktree add <base>/<slug>` (tracking
   `<remote>/<branch>` when remote-only; prompts to create from
   `<remote>/<default>` — `--create`/`--no-create` — when the name matches
   nothing), copies `source.git.copy`
    includes (if any), assigns the
    next index, allocates `base + index*step` ports, ensures managed keys in
    `.env` (append-only, never overwrites your values), appends
   `{branch, slug, absPath, index, ports, composeProject, status}` to
   `<worktreeBase>/.wrk3-state.json` (absolute paths → cwd-independent).
   `add --remote <name> [--mine]` bulk-creates from a remote (default:
   `source.git.remote`, else `origin`), skipping already registered or
   checked-out branches.
2. `wrk3 up` → runs `entry.setup` commands (`sh -c`, `cwd=worktree`,
    `env=ports`), then `docker compose -p <prefix>-<slug> up`, then
    `entry.run` — in parallel across worktrees via errgroup with prefixed logs.
    Bare `up`/`down` apply to all worktrees including the implicit main
    checkout (repo root, reserved port index -1, managed `.env` section ensured on run);
    pass names to filter.
3. `wrk3 status` → reads the state file plus the implicit main checkout,
    probes live runner status, prints
    `WORKTREE/BRANCH/STATUS/PORTS/COMPOSE_PROJECT` (plus `URL` when
    `proxy.enabled` → `http://<slug>.localhost:<port>` via the stdlib
    gateway; `up` auto-starts it, `wrk3 proxy status|open|hosts-sync`
    manage it).
4. `wrk3 remove` → `compose down -v` + `git worktree remove` + state cleanup.
    `remove` never touches main (explicit `remove <main-branch>` is refused;
    `remove --all` covers only managed worktrees).

## Development

```bash
go build ./... && go vet ./... && go test ./... -count=1
# or
make test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for workflow and commit style, and
[docs/INSTALL.md](docs/INSTALL.md) / [docs/CONFIGURATION.md](docs/CONFIGURATION.md) /
[docs/USAGE.md](docs/USAGE.md) for setup and usage. [ROADMAP.md](ROADMAP.md)
tracks the any-codebase project goal and runner coverage. To add a new `Source` or
`Runner` backend, see [docs/PLUGINS.md](docs/PLUGINS.md).

## License

[MIT](LICENSE) © 2026 Mijat Miletic.
