# wrk3

[![CI](https://github.com/mytmlt/wrk3/actions/workflows/ci.yml/badge.svg)](https://github.com/mytmlt/wrk3/actions/workflows/ci.yml)
[![Release](https://github.com/mytmlt/wrk3/actions/workflows/release.yml/badge.svg)](https://github.com/mytmlt/wrk3/actions/workflows/release.yml)
[![Go](https://img.shields.io/badge/go-1.26-blue)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Run **multiple branches of the same repo in parallel** as git worktrees, each
isolated with its own ports and container project — from any directory.

`wrk3` was built for stacks where one checkout = one full environment
(e.g. a `docker compose` dev stack): point it at a repo, `add` two branches,
`up --all`, and get two running copies on distinct ports and distinct compose
projects. `status` shows every worktree, branch, and port at a glance.

## Features

- **Parallel worktrees** — `git worktree add/remove/list` behind a `Source`
  interface (`git` ships; the shape reserves future backends).
- **Isolated runners** — `docker compose -p <prefix>-<slug>` per worktree
  behind a `Runner` interface (`docker` ships; `portainer`/`nomad` stubs
  return `not implemented`).
- **Deterministic ports** — `allocated = base + index * step` per port name,
  written to each worktree's `.env`; `status` reads them back from the state
  file (`?`/`stale` when the directory is missing).
- **Project registry** — named pointers to `wrk3.yaml` in
  `~/.config/wrk3/projects.yaml`; every command resolves
  `--project` > `$WRK3_PROJECT` > current project > cwd scan, so it works
  from any directory.
- **Single static binary** — Go, no runtime deps besides `git` and `docker`.

## Install

Requires Go ≥ 1.26 only for the `go install` path; `git` and `docker` at
runtime. Full details: [docs/INSTALL.md](docs/INSTALL.md).

```bash
# Option 1 — release assets (linux/darwin/windows, checksums verified)
curl -fsSL https://raw.githubusercontent.com/mytmlt/wrk3/main/scripts/install.sh | bash

# Option 2 — Go toolchain
go install github.com/mytmlt/wrk3@latest
wrk3 version

# Option 3 — from source
git clone https://github.com/mytmlt/wrk3.git && cd wrk3
make install            # installs to /usr/local/bin (PREFIX overridable)
wrk3 version
```

Shell completions: `make completion` then source
`completions/wrk3.<bash|zsh|fish|powershell>`.

## Quickstart (5 minutes)

```bash
# 1. Describe your repo (see wrk3.yaml.example + docs/CONFIGURATION.md)
cp wrk3.yaml.example myproject.yaml
$EDITOR myproject.yaml   # config lives in the repo root; no repo path needed

# 2. Register it (works from any cwd afterwards)
wrk3 project add myproject --config "$PWD/myproject.yaml"
wrk3 project use myproject

# 3. Fetch remote branches, create two isolated worktrees
wrk3 fetch
wrk3 fetch --mine                    # only your branches (tip author = git config user.name/user.email)
wrk3 fetch --author alice            # substring match on author name/email
wrk3 add feature-a feature-b
# or pick interactively: wrk3 add --select

# 4. Boot both stacks in parallel (setup entries, compose up, run entry)
wrk3 up --all

# 5. Inspect, tail, run commands
wrk3 status
wrk3 logs feature-a -f
wrk3 exec feature-a -- make test

# 6. Tear down
wrk3 down --all
wrk3 remove feature-a feature-b
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

> **Agents / automation:** the [`wrk3-setup` skill](skills/wrk3-setup/SKILL.md)
> knows how to author and validate a `wrk3.yaml` for any repo.

## How it works

1. `wrk3 add <branch>` → `git worktree add <base>/<slug>`, assigns the
   next index, allocates `base + index*step` ports, writes `.env`, appends
   `{branch, slug, absPath, index, ports, composeProject, status}` to
   `<worktreeBase>/.wrk3-state.json` (absolute paths → cwd-independent).
2. `wrk3 up` → runs `entry.setup` commands (`sh -c`, `cwd=worktree`,
   `env=ports`), then `docker compose -p <prefix>-<slug> up`, then
   `entry.run` — in parallel across worktrees via errgroup with prefixed logs.
3. `wrk3 status` → reads the state file, probes live runner status, prints
   `PROJECT/WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT`.
4. `wrk3 remove` → `compose down -v` + `git worktree remove` + state cleanup.

## Development

```bash
go build ./... && go vet ./... && go test ./... -count=1
# or
make test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for workflow and commit style, and
[docs/INSTALL.md](docs/INSTALL.md) / [docs/CONFIGURATION.md](docs/CONFIGURATION.md) /
[docs/USAGE.md](docs/USAGE.md) for setup and usage. To add a new `Source` or
`Runner` backend, see [docs/PLUGINS.md](docs/PLUGINS.md).

## License

[MIT](LICENSE) © 2026 Mijat Miletic.
