# wrk3 agent skill: set up wrk3.yaml for this app

Goal: turn **any codebase** into a working `wrk3.yaml` that runs the app
the way its developers run it locally — today via the `docker` runner,
tomorrow via `portainer` / `nomad` / bare-machine runners (see
`github.com/mytmlt/wrk3/blob/main/ROADMAP.md`). You are setting up `wrk3` (parallel git worktrees, each
with isolated ports and its own runner project) for the app in the
current repo. Work from the repo root (the config always lives in the
project root). No config exists yet — that is what you are authoring.

Ground rule: **learn the manual setup first, then encode it.** Every
`entry.*` command and `source.git.copy` entry must trace back to
onboarding evidence (`file:line`) or an explicit developer answer.
Never invent setup commands. When in doubt, **ask the developer** —
a short question now beats a broken config later.

## Phase 0 — learn how developers run it locally (do this first)

Reconstruct the onboarding flow before touching `wrk3.yaml`:

1. Read the onboarding docs: `README.md`, `CONTRIBUTING.md`, `AGENTS.md`,
   `docs/*`, `*.md` setup guides, `Makefile` help targets,
   `package.json` / `pyproject.toml` / `go.mod` / `Cargo.toml` scripts,
   `docker-compose.yml` / `compose.yaml` / `compose.*.yml`,
   `.env.example` / `.env.sample`, onboarding scripts (`setup.sh`,
   `bin/setup`, `scripts/bootstrap*`).
2. Write down the manual sequence in order: prerequisites (toolchains,
   docker, system deps) → env files (what to copy, what secrets to fill)
   → install (`npm ci`, `pip install`, `go mod download`, ...) →
   services (`docker compose up`, DB, cache, ...) → migrations/seed →
   dev server / app entrypoint → tests / health check. Cite `file:line`
   for each step.
3. Classify the runtime:
   - Compose stack (one or more compose files)? Note which file is the
     dev stack and which services bind host ports.
   - Bare-machine process (`go run ./...`, `npm run dev`, `make run`,
     binary, script)? Note the exact command and working directory.
   - Both (infra in compose, app on the host)? Note the split.
4. List the local-state files a fresh clone is missing but a running dev
   checkout has: `.env.local`, `certs/`, `storage/*.sqlite`, `data/`,
   `node_modules/` (usually rebuilt, not copied). These become
   `source.git.copy` candidates — small, slow-to-recreate, or secret
   bearing. Large rebuildable dirs (`node_modules`, `.venv`, build
   output) stay out; the setup/install step recreates them.

Ask the developer when any of these is true — do not guess:

- The onboarding docs are missing, contradictory, or stale.
- There are multiple plausible entrypoints (several compose files,
  several `run`/`dev`/`start` scripts) and no clear dev default.
- Secrets or env values have no example file and you cannot tell what
  the app needs (which vars, what format).
- It is unclear which service/port is the "app" (for `ports.base.app`),
  or which host ports must stay unique per worktree.
- The app seems to need shared mutable host state (bind-mounted DB
  dir, host network, fixed global port) that parallel worktrees would
  collide on.

Ask concisely, propose what you found, and wait for the answer before
authoring the config.

## Phase 1 — compatibility triage (read-only)

Map the Phase 0 findings onto wrk3 isolation:

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
3. No compose stack? The app is a bare-machine candidate: record the
   exact host commands from Phase 0 and note that only the `docker`
   runner ships today (`portainer` / `nomad` / bare-machine runners are
   roadmap stubs returning `not implemented`). Still author `wrk3.yaml`
   with accurate `entry.*` so the local flow is captured and `exec`
   works; pick `runner.type: docker` only when a compose stack exists.
4. Check local setup: `Makefile`, `README.md`, `package.json` scripts.
   Confirm the dev server / tests boot commands you will wire into
   `entry.run` / `entry.setup` / `entry.stop`.

Emit a verdict before writing config:
`COMPATIBLE` / `COMPATIBLE WITH CHANGES` (list the edits) /
`INCOMPATIBLE` (state why). Do not author `wrk3.yaml` on INCOMPATIBLE.

## Phase 2 — author wrk3.yaml

Write `wrk3.yaml` in the repo root. Start from this template, adapt
every field to the Phase 0 evidence, and keep the citations in your
summary (not in the YAML):

```yaml
project:
  worktreeBase: .worktrees        # required; relative => resolved against repo root
source:
  type: git
  git:
    remote: origin
    fetchPrune: true
    # copy: [".env.local", "certs/"]  # optional gitignored files/dirs into each new worktree on add
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
  `source.git.copy` is an optional list of repo-relative files/dirs
  (globs, `**` supported) copied into each new worktree on `add`
  (missing skips with a warning, existing files never overwritten).
  Derive it from Phase 0 local-state findings, not guesses.
- `runner.type` must be `docker` today (`portainer` / `nomad` are
  intentional `not implemented` stubs; bare-machine is roadmap — see
  `github.com/mytmlt/wrk3/blob/main/ROADMAP.md`). `runner.docker.composeFiles` needs at least one file;
  `runner.docker.projectPrefix` is required. Never set
  `container_name:` in compose files.
- `entry.*` strings execute verbatim via `sh -c` with `cwd=worktree`
  and `env=allocated ports`. `entry.run` and `entry.stop` are required.
  `entry.setup` is an ordered list; empty strings are skipped. Each
  command must mirror a Phase 0 manual step (install →
  migrate/seed → up → run); never hardcode repo-specific commands you
  did not find in onboarding or confirm with the developer.
- `ports.base` defaults to `{app: 8000}`, `ports.step` defaults to `100`.
  Each port name becomes `<NAME>_PORT` in `.env` (uppercased,
  non-alphanumerics to `_`: `app` -> `APP_PORT`). `BASE_URL`,
  `WEBHOOKS_BASE_URL`, `ALLOWED_WS_ORIGINS` derive from the `app` port
  (`http://localhost:<app>`); `APP_URL` (`http://<slug>.<domain>[:port]`)
  is added only when `proxy.enabled` (stdlib gateway, default
  `localhost`/`127.0.0.1:8080`, `up`/`add`/dashboard auto-start it). Compose/`entry` commands must consume the
  matching `.env` vars (e.g. `"${APP_PORT:-8000}:8000"`). Only declare
  port names the stack actually binds.
- Branch slugs: `feature/foo` -> `feature-foo` (max 50 chars).
- Personal variants belong in `wrk3.<name>.local.yaml` (gitignored);
  commit only shareable templates like `wrk3.yaml` or `wrk3.yaml.example`.

## Phase 3 — validate

```bash
wrk3 -f ./wrk3.yaml status   # config errors fail fast, listing valid source/runner types
```

`status` is read-only (never touches `.env`). Only then proceed to
`wrk3 fetch`, `wrk3 add <branch>`, `wrk3 up` — and run `add`/`up` only
against throwaway repos/worktrees, cleaning up with `down` + `remove`
afterwards; never experiment on the developer's real checkouts. Every
`add`/`up` ensures the worktree `.env` contains the managed port
section (append-only; existing lines and secrets are never overwritten).
Report back: onboarding sources cited, developer answers used, the
verdict, and what you changed in the repo to make the stack wrk3-ready.
