# Configuration reference (`wrk3.yaml`)

Start from [`wrk3.yaml.example`](../wrk3.yaml.example). Validate any
config with `wrk3 -f <path> status` — config errors fail fast and
list the available `source.type` / `runner.type` options.

## Full example

```yaml
project:
  worktreeBase: .worktrees        # required; relative => resolved against repo root (config dir)
source:
  type: git
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]  # at least one
    projectPrefix: demo                 # used as compose -p <prefix>-<slug>
entry:
  setup: ["docker compose up --wait --build"]  # run in order before compose up
  run: "docker compose logs -f"                # required; run after compose up
  stop: "docker compose down"                  # required; used by down
  logs: "docker compose logs -f"
ports:
  base: {app: 8000}
  step: 100
```

## Fields

| Path | Required | Notes |
| ---- | -------- | ----- |
| `project.worktreeBase` | yes | Directory holding worktrees + `.wrk3-state.json`. The repo root is the directory containing `wrk3.yaml` (the config always lives in the project root). Relative values resolve against the repo root; absolute values pass through. Worktrees are created with `git -C <repoRoot>`. |
| `source.type` | yes | `git` (only backend that ships). Unknown values error listing `[git]`. |
| `source.git.remote` | no | Default `origin`. Used by `fetch` (`fetch --prune`) and ref listing. `--remote <name>` on `fetch`/`add` overrides it per-invocation; `--mine` on `add` requires remote mode (uses the flag or this value). Remote-only branches are created as tracking branches (`--track -b`). |
| `source.git.fetchPrune` | no | Default `true`. |
| `runner.type` | yes | `docker` (ships). `portainer`/`nomad` exist as stubs → `not implemented`. Unknown values error listing available runners. |
| `runner.docker.composeFiles` | yes (docker) | At least one compose file, resolved inside each worktree. Compose files must not set `container_name:` — it is global on the daemon and bypasses `-p <prefix>-<slug>` isolation, so `up` fails fast naming the offending file/services. Compose generates `<project>-<service>-1` automatically. |
| `runner.docker.projectPrefix` | yes (docker) | Compose project becomes `<prefix>-<slug>` → free volume/network isolation. |
| `entry.setup` | no | Ordered list, each run via `sh -c` with `cwd=worktree`, `env=allocated ports`. Empty strings skipped. |
| `entry.run` | yes | Long-running command started after compose up (e.g. dev server). |
| `entry.stop` | yes | Used by `down`. |
| `entry.logs` | no | Used by `logs`. |
| `ports.base` | no | Defaults to `{app: 8000}`. `app` is required; add more names when the stack binds extra host ports. |
| `ports.step` | no | Default `100`. Allocation: `allocated[name] = base[name] + index*step`. |

## Ports and `.env`

- Each `add` takes the next index (`max(index)+1`, starting at 0) and writes
  the allocation into the worktree's `.env` plus the state file.
- The repo-root main checkout is implicit (no state entry) with reserved
  index `-1` (e.g. `7900` with `base: {app: 8000}, step: 100`). Its `.env`
  is written on `up`/`down`/`exec`. Port/project collisions with managed
  worktrees surface as errors.
- `.env` variable mapping (`internal/ports`, `cmd/common.go`): every port
  name becomes `<NAME>_PORT` (uppercased, non-alphanumerics → `_`), sorted
  for stable output. Examples: `app` → `APP_PORT`, `web` → `WEB_PORT`.

Derived: `BASE_URL`, `WEBHOOKS_BASE_URL`, `ALLOWED_WS_ORIGINS` =
`http://localhost:<app>`. `COMPOSE_PROJECT_NAME` is forced by the docker
runner (not set in `.env`).

Only use the port names your compose files read — extra names are
harmless. To adapt: change `ports.base` keys/values and make sure your
compose/`entry` commands consume the matching `.env` vars (e.g.
`"${APP_PORT:-8000}:8000"`).

## Config discovery + project registry

Like `docker compose`: every command resolves its config as
`-f/--file <path>` > upward scan from cwd for `wrk3.yaml`, then
`wrk3.yml` (nearest directory wins; `wrk3.yaml` preferred in the same
directory). `cd` into the repo (or a subdirectory) or pass `-f`.

`wrk3 add` also registers the repo in `~/.config/wrk3/projects.yaml`
(XDG-aware, `$WRK3_CONFIG_HOME` override) under the repo root
directory name, refreshing `lastSeen`. `wrk3 project ls` lists the
registry; `wrk3 ls --project <name>` reads that project's state from
anywhere. Registry writes are best-effort (warn on stderr, never fail
`add`); same basenames stay distinct by config path and `ls --project`
errors ambiguous instead of guessing.

Runtime state (`<worktreeBase>/.wrk3-state.json`) lives next to the repo.

## Local configs

Place `wrk3.yaml` (or personal variants) in the repo root — the config
directory is the repo root, so no `repo` path is stored. Name personal
variants `wrk3.<name>.local.yaml` — gitignored. Commit only shareable
templates like `wrk3.yaml.example`.
