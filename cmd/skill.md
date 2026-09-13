# wrk3 agent skill: set up wrk3.yaml for this app

You are setting up `wrk3` (parallel git worktrees, each with isolated
ports and its own compose project) for the app in the current repo.
Work from the repo root (the config always lives in the project root).
No config exists yet — that is what you are authoring.

## Phase 0 — compatibility triage (read-only, do this first)

Inspect the project's docker compose files and local setup:

1. Find compose files: `docker-compose.yml`, `compose.yaml`, `compose.*.yml`.
2. Check each service for:
   - `container_name:` — INCOMPATIBLE as-is. It is global on the daemon
     and bypasses `docker compose -p <prefix>-<slug>` isolation. It must
     be deleted (compose generates `<project>-<service>-1` automatically).
     `wrk3 up` fails fast on this; cite `file:line` evidence.
   - Hardcoded host ports (`"8000:8000"`, `ports: [5432:5432]`) —
     COMPATIBLE WITH CHANGES. They must become `${VAR:-default}`
     references (e.g. `"${APP_PORT:-8000}:8000"`) so each worktree gets
     its own host port. Cite `file:line`.
   - Bind mounts (`volumes: ["./data:/data"]`, relative host paths) —
     flag when two worktrees would share mutable host state; named
     volumes are fine (isolated per compose project).
   - `network_mode: host` — INCOMPATIBLE with port isolation; flag it.
3. Check local setup: `Makefile`, `README.md`, `package.json` scripts.
   Note how the dev server / tests boot (you will wire this into
   `entry.run` / `entry.setup`).

Emit a verdict before writing config:
`COMPATIBLE` / `COMPATIBLE WITH CHANGES` (list the edits) /
`INCOMPATIBLE` (state why). Do not author `wrk3.yaml` on INCOMPATIBLE.

## Phase 1 — author wrk3.yaml

Write `wrk3.yaml` in the repo root. Start from this template and adapt it:

```yaml
project:
  worktreeBase: .worktrees        # required; relative => resolved against repo root
source:
  type: git
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]  # at least one; resolved inside each worktree
    projectPrefix: demo                 # compose project becomes <prefix>-<slug>
entry:
  setup: ["docker compose up --wait --build"]  # ordered list, run before compose up
  run: "docker compose logs -f"                # required; long-running, after compose up
  stop: "docker compose down"                  # required; used by `wrk3 down`
  logs: "docker compose logs -f"               # optional; used by `wrk3 logs`
ports:
  base: {app: 8000}               # `app` is required; add names per extra host port
  step: 100                       # allocation: allocated[name] = base[name] + index*step
```

Rules:

- `project.worktreeBase` is required. Relative paths resolve against the
  repo root (the config directory); absolute paths pass through.
- `source.type` must be `git` (only backend that ships).
- `runner.type` must be `docker`. `runner.docker.composeFiles` needs at
  least one file; `runner.docker.projectPrefix` is required. Never set
  `container_name:` in compose files.
- `entry.*` strings execute verbatim via `sh -c` with `cwd=worktree`
  and `env=allocated ports`. `entry.run` and `entry.stop` are required.
  `entry.setup` is an ordered list; empty strings are skipped.
- `ports.base` defaults to `{app: 8000}`, `ports.step` defaults to `100`.
  Each port name becomes `<NAME>_PORT` in `.env` (uppercased,
  non-alphanumerics to `_`: `app` -> `APP_PORT`). `BASE_URL`,
  `WEBHOOKS_BASE_URL`, `ALLOWED_WS_ORIGINS` derive from the `app` port
  (`http://localhost:<app>`). Compose/`entry` commands must consume the
  matching `.env` vars (e.g. `"${APP_PORT:-8000}:8000"`). Only declare
  port names the stack actually binds.
- Branch slugs: `feature/foo` -> `feature-foo` (max 50 chars).
- Personal variants belong in `wrk3.<name>.local.yaml` (gitignored);
  commit only shareable templates like `wrk3.yaml` or `wrk3.yaml.example`.

## Phase 2 — validate

```bash
wrk3 -f ./wrk3.yaml status   # config errors fail fast, listing valid source/runner types
```

`status` is read-only (never touches `.env`). Only then proceed to
`wrk3 fetch`, `wrk3 add <branch>`, `wrk3 up`. Every `add`/`up` ensures
the worktree `.env` contains the managed port section (append-only;
existing lines and secrets are never overwritten).
