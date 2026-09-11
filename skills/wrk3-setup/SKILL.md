---
name: wrk3-setup
description: Author and validate a wrk3.yaml config for any repo so multiple branches run in parallel as isolated git worktrees. Use when setting up wrk3 for a project, adding a new stack, or fixing a broken config.
---

# wrk3 Setup

Goal: a working `wrk3.yaml` for the target repo, registered with
`wrk3 project add`, verified through `fetch` + `status` (and `add`/`up`
when the user approves running real workloads).

Rule 0: `wrk3.yaml` is executable configuration — `entry.setup/run/stop`
run via `sh -c` on the user's host. Only write commands taken from the repo's
own docs/Makefile/compose files. Never invent setup steps.

## 1. Inspect the repo

Collect these facts before writing anything:

1. **Repo root.** The config always lives in the repo root, so no repo
   path is stored — just place `wrk3.yaml` there. Get the root with
   `git rev-parse --show-toplevel` inside the checkout.
2. **Compose files.** List candidates: `ls docker-compose*.yml compose*.yaml`.
   Read each one and note: service names, published host ports (`ports:`),
   env vars consumed (`${VAR:-default}`), named volumes, and networks.
3. **Dev lifecycle.** Read `Makefile`, `package.json` scripts, and
   `README.md`/`CONTRIBUTING.md` for the canonical commands:
   - boot deps/infra (e.g. `docker compose up --wait --build`)
   - first-time setup (install, codegen, migrations, seed)
   - foreground dev server (e.g. `npm run dev`, `docker compose logs -f`)
   - stop (e.g. `docker compose down`)
   - logs (e.g. `docker compose logs -f`)
4. **Host ports the stack binds.** Every host-side port that must differ per
   worktree needs a `ports.base` entry. Map each to the `.env` var the
   compose file actually reads (see port table below).
5. **Branch names** the user wants in parallel (for slug/validation context).

If anything is ambiguous (which compose file is canonical, what the setup
sequence is, which ports matter), ask — do not guess entry commands.

## 2. Author `wrk3.yaml`

Start from the template in `examples/compose-stack.yaml` (this skill dir) or
the repo's `wrk3.yaml.example`. Write the new config to a **local,
gitignored** path in the repo root (`wrk3.<name>.local.yaml`) unless the
user explicitly wants a committed template — the config must live in the
repo root (its directory is the repo root).

```yaml
project:
  worktreeBase: .worktrees           # relative => <repoRoot>/.worktrees
source:
  type: git                          # only backend that ships in v1
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker                       # portainer/nomad are stubs (not implemented)
  docker:
    composeFiles: [docker-compose.yml]  # must exist in the repo
    projectPrefix: demo                 # lowercase, short; compose -p <prefix>-<slug>
entry:
  setup: ["docker compose up --wait --build"]  # verbatim repo commands, in order
  run: "docker compose logs -f"
  stop: "docker compose down"
  logs: "docker compose logs -f"
ports:
  base: {app: 8000}                # every host port your stack binds (+ `app` required)
  step: 100                        # allocation = base + index*step
```

Constraints (enforced by `config.Validate` — unknown values fail fast):

- `source.type` must be `git`. `runner.type` must be `docker` for real runs.
- `runner.docker.composeFiles` non-empty; `projectPrefix` non-empty.
- `entry.run` and `entry.stop` non-empty. Each entry string runs via
  `sh -c` with `cwd=<worktree>`, `env=<allocated ports>`.
- `project.worktreeBase`: relative resolves against the repo root
  (the directory containing `wrk3.yaml`);
  absolute passes through.

### Port mapping (`ports.base` → `.env` vars)

Only include names your stack actually binds; extra names are harmless.
`app` is required (derived URLs and the `status` APP column build from
it). Every name becomes `<NAME>_PORT` (uppercased, non-alphanumerics →
`_`): `app` → `APP_PORT` (+ `BASE_URL`, `WEBHOOKS_BASE_URL`,
`ALLOWED_WS_ORIGINS` = `http://localhost:<app>`), `web` → `WEB_PORT`.

Two rules for parallel safety:

1. The compose files **must** consume these vars for host-port bindings
   (e.g. `"${APP_PORT:-8000}:8000"`). If a compose file hardcodes a
   host port, either parameterize it first or accept the conflict and say so.
2. Two configs sharing one host need distinct `ports.base` offsets or steps,
   otherwise worktree 0 of project A collides with worktree 0 of project B.

## 3. Register and validate (read-only first)

```bash
wrk3 project add <name> --config <path-to-yaml>
wrk3 --project <name> fetch    # git fetch --prune + list origin/* refs
wrk3 --project <name> status   # renders empty table on a fresh config
```

Both must exit 0. Typical failures and fixes:

| Error | Fix |
| ----- | --- |
| `project.worktreeBase must not be empty` | Set `project.worktreeBase` (e.g. `.worktrees`). |
| `unknown source/runner type` | Must be `git` / `docker`; check spelling. |
| `runner.docker.composeFiles must list at least one` | Add the compose file found in step 1. |
| `entry.run must not be empty` | Fill `run` and `stop` from repo docs. |
| `load config ... no such file` | Registry points at a moved file — re-`add` with the right `--config`. |

## 4. Live verification (needs user approval)

`add` creates real worktrees; `up` boots real containers. Confirm before
running, especially on heavy stacks (double `setup` can take minutes).

```bash
wrk3 --project <name> add <branch>            # worktree + ports + .env
wrk3 --project <name> up <branch>             # setup → compose up → run
wrk3 --project <name> status                  # expect running + distinct ports
wrk3 --project <name> down <branch>
wrk3 --project <name> remove <branch>         # compose down -v + worktree remove
```

For a second parallel instance, `add`/`up` another branch and confirm
`status` shows distinct ports and distinct `<prefix>-<slug>` compose projects.

## 5. Hand off

- Show the user the final config path, project name, and `status` output.
- If the config is meant to be shared, copy it to `wrk3.yaml.example`
  shape — never commit personal local configs.
- Never commit `wrk3.<name>.local.yaml`, `.wrk3-state.json`,
  `.worktrees/`, or `.env` files.
