---
name: wrk3-setup
description: Set up wrk3 parallel worktrees for a repo — check compose/setup compatibility, propose minimal fixes when incompatible, then author and validate wrk3.yaml. Use when setting up wrk3, evaluating a repo, adding a stack, or fixing port/name conflicts or a broken config.
wrk3-version: v0.10.25
---

# wrk3 Setup

Goal: a working `wrk3.yaml` for the target repo, verified through
`fetch` + `status` (and `add`/`up` when the user approves running real
workloads). No registration step — wrk3 finds `wrk3.yaml`/`wrk3.yml`
walking up from cwd (override with `-f/--file <path>`).

If the repo is not compatible as-is, propose the minimal changes that
would make it compatible — then apply them (or work around them) only
with user approval before authoring the config.

Rule 0: `wrk3.yaml` is executable configuration — `entry.setup/run/stop`
run via `sh -c` on the user's host. Only write commands taken from the repo's
own docs/Makefile/compose files. Never invent setup steps. Every
compatibility claim must cite the file + line that proves it
(e.g. `docker-compose.yml:12`, `Makefile:8`, `README.md:20`).

Canonical source: this skill is vendored in the `wrk3` repo at
`skills/wrk3-setup/` (see also `wrk3.yaml.example` and
`docs/CONFIGURATION.md`). When CLI behavior changes, update this skill
together with the docs and `CHANGELOG.md` — never leave them out of sync
(see `AGENTS.md`).

## 0. Compatibility triage (read-only — run first, change nothing)

### 0a. Collect the facts

Stop and report if the repo root can't be found.

1. **Repo root + git state.** `git rev-parse --show-toplevel` (config must
   live in the repo root). Note uncommitted compose changes — they affect
   the verdict.
2. **Compose candidates.** `ls docker-compose*.yml docker-compose*.yaml compose*.yaml compose*.yml`.
   Read every file found. For each service record:
   - `image:` / `build:` (what it is)
   - `ports:` host bindings — verbatim strings (e.g. `"${APP_PORT:-4000}:3000"`
     vs `"4000:3000"`)
   - `container_name:` — exact value (any non-empty value is a blocker;
     wrk3's `up` fails fast on it because the name is global on the daemon
     and bypasses `compose -p <prefix>-<slug>` isolation)
   - `environment:` entries consuming `${VAR}` / `${VAR:-default}`
   - `volumes:` — named volumes (safe: isolated per compose project
     `<prefix>-<slug>`) vs **bind mounts**: relative `./x:/y`
     (worktree-safe, each worktree gets its own copy) vs absolute
     `/data:/y` or `~/x:/y` (shared across worktrees → corruption risk)
   - `network_mode: host` / `privileged: true` / `ports` with host IP
     pinning — flag each, they break isolation
   - `extends:` / multiple `-f` files / `profiles:` — note composition order
   - `healthcheck:` — fine, probed automatically (display-only, see §2)
3. **Local setup lifecycle.** Read `Makefile`, `package.json` scripts,
   `README.md`, `CONTRIBUTING.md`, `Dockerfile(s)`, `.env.example`,
   migration dirs (`db/`, `migrations/`, `drizzle/`, `prisma/`). Record the
   canonical:
   - boot deps/infra (usually `docker compose up --wait --build`)
   - first-time setup (install, codegen, migrations, seed — exact commands)
   - foreground dev command (what `entry.run` should be)
   - stop / logs commands
   If setup needs host-global state (single system DB, fixed socket path,
   license server on a hardcoded port, hardware device), record it — it may
   be incompatible (run that service outside wrk3 or use single-worktree mode).
4. **Host ports the stack binds.** Every host-side port that must differ per
   worktree. For each, note whether compose reads it from an env var
   (`${APP_PORT:-4000}` → parameterizable, good) or hardcodes it
   (`4000:3000` → conflict across worktrees until parameterized).
   Also note URL-shaped values with an explicit port (e.g. `BASE_URL` on
   `http://localhost:8000`): those belong in `urls:` — each entry takes its
   own lowest free port in its `range`.
5. **Gateway / health intent.** Note whether the user wants
   `http://<slug>.<domain>` URLs (`proxy.enabled`) or per-check status
   suffixes (`health.checks`). Both are opt-in and display/runner-env only.

If anything is ambiguous (which compose file is canonical, what the real
setup sequence is), ask — do not guess. Never run `add`/`up` during
triage (they create worktrees/containers).

### 0b. Evaluate against the wrk3 isolation model

wrk3 gives each worktree: a git worktree dir, a monotonic index (`max+1`
floored at `1`; index `0` is the implicit repo-root main checkout at
exactly the `ports.base` allocation) plus per-service range ports
(lowest free in `[base, max]` with gap reuse and a `127.0.0.1` bind probe,
skipping OS-occupied ports) injected as env (`<NAME>_PORT`, uppercased),
and a compose project `-p <prefix>-<slug>` (empty `projectPrefix` means
slug-only `-p <slug>`; volumes/networks/container names derive from it).
A project works with wrk3 iff all of these hold (or can be made to hold
with listed changes):

| # | Check | How | Blocker if |
| - | ----- | --- | ---------- |
| C1 | No `container_name:` | grep each compose file | any non-empty `container_name:` → `COMPATIBLE WITH CHANGES` (delete it; compose generates `<project>-<service>-1`) |
| C2 | Host ports parameterized | every `ports:` host side uses `${VAR:-default}` | hardcoded `"HOST:CTR"` → `COMPATIBLE WITH CHANGES` (parameterize to `"${X_PORT:-HOST}:CTR"`) |
| C3 | No `network_mode: host` | grep compose files | present → usually `INCOMPATIBLE` (ports can't be isolated per worktree) |
| C4 | No shared absolute bind mounts | inspect `volumes:` | absolute host path shared by all worktrees holding mutable state → `COMPATIBLE WITH CHANGES` (convert to named volume or relative path) or `INCOMPATIBLE` if the path is mandated by the toolchain |
| C5 | Setup commands are repo verbs | setup comes from Makefile/README/compose, runs via `sh -c` with `cwd=worktree`, `env=ports` | setup requires manual GUI steps, host-global daemons, or secrets you can't reproduce per worktree → `INCOMPATIBLE` or scoped `COMPATIBLE WITH CHANGES` |
| C6 | Ports fit `base` + `ranges` | every `ports.base` key has a `ranges` entry (`app` required, `base` inside `[min,max]`, `1–65535`, `min <= max`); legacy `ports.step` is a hard error | `step` present, `app` missing, `base` outside its range, or two configs sharing a host with overlapping bases/ranges → `COMPATIBLE WITH CHANGES` (delete `step`, add `ranges: {app: [8000, 8099]}`, shift `ports.base` / `ranges`) |
| C7 | `urls` / `proxy` / `health` shape | `urls[].base` absolute URL with explicit port inside its `range`; `proxy.addr` `host:port` with port ≥ 1024 (privileged ports rejected); `health.checks[].timeout` `1s–120s` | malformed entry → `COMPATIBLE WITH CHANGES` (fix the block or drop it) |
| C8 | Heavyweight sanity | note image sizes, `setup` time, ports count | >2 min boot or >5 host ports → still compatible, but warn and require user approval before live `up` |

Named volumes, per-service `networks:`, `depends_on:`, and `healthcheck:`
are fine — they inherit `-p <prefix>-<slug>` isolation automatically.
Two `ports.base` names may share one value as aliases for a single host
port (e.g. `{app: 8000, public_api: 8000}`): aliases are allocated once
and stay equal — do not "fix" them by splitting. Names mapping to the
same `<NAME>_PORT` variable (e.g. `api-v2` and `api_v2`) are rejected.

### 0c. Emit the verdict

Use the template in `examples/verdict-template.md` (copy into chat or a
scratch file — never commit it):

```markdown
## wrk3 compat verdict: <COMPATIBLE | COMPATIBLE WITH CHANGES | INCOMPATIBLE>

**Repo root:** <path> | **Compose files:** <list> | **Services:** <names>

| Check | Result | Evidence |
| ----- | ------ | -------- |
| C1 container_name | pass/fail | <file:line> |
| C2 host ports parameterized | pass/fail | <file:line> |
| C3 host network mode | pass/fail | <file:line> |
| C4 bind mounts | pass/fail | <file:line> |
| C5 setup reproducible | pass/fail | <file:line> |
| C6 base+ranges | pass/fail | <bases + ranges> |
| C7 urls/proxy/health | pass/fail | <block or n/a> |

**Required changes (if any):**
1. <file:line — exact edit, e.g. `delete container_name: testapp-db`>
2. <e.g. `"4000:3000"` → `"${APP_PORT:-4000}:3000"` + `ports.base: {app: 4000}` + `ranges: {app: [4000, 4099]}`>

**Proposed ports.base mapping:**
| wrk3 name | .env var | compose host binding | default |
| --------- | -------- | -------------------- | ------- |
| app | APP_PORT | <service ports entry> | <n> |

**Suggested entry commands (repo verbs only):**
setup: [...] | run: "..." | stop: "..." | logs: "..." | reload: [...]
```

Verdict rules:

- All C1–C7 pass → `COMPATIBLE` — continue to §1.
- Any fail fixable by editing compose/env/ports (C1, C2, C4-path, C6, C7) →
  `COMPATIBLE WITH CHANGES` — continue to §1 (remediation), not §2.
  Do not apply edits without user approval.
- C3 present, or C5 requires unreproducible host-global state →
  `INCOMPATIBLE` — stop. Say why, name the tool constraint, suggest the
  closest alternative (e.g. run that one service outside wrk3 or in
  single-worktree mode). Do not author a config.

## 1. Remediation (only for `COMPATIBLE WITH CHANGES` — needs approval)

1. List every required edit with file:line evidence (e.g. delete
   `container_name:`, `"4000:3000"` → `"${APP_PORT:-4000}:3000"`,
   absolute bind → named volume, shifted `ports.base`/`ranges`, deleted
   `ports.step`, fixed `urls`/`proxy` block).
2. Get user approval before applying anything or working around it in the
   wrk3 config. Say which edits you will make vs. which conflicts the user
   accepts.
3. After approval, apply the edits (or the agreed workaround), then re-run
   the §0 checks on the changed files before continuing — only proceed to
   §2 with a `COMPATIBLE` (or approved `WITH CHANGES`) verdict.

## 2. Inspect the repo

Collect these facts before writing anything (reuse the §0 output —
don't re-read files you already cited):

1. **Repo root.** The config always lives in the repo root, so no repo
   path is stored — just place `wrk3.yaml` there. Get the root with
   `git rev-parse --show-toplevel` inside the checkout.
2. **Compose files.** Confirmed list from §0: service names, published
   host ports (`ports:`), env vars consumed (`${VAR:-default}`), named
   volumes, and networks. Pick the runner backend: `docker` or `podman`
   (both ship; `portainer`/`nomad` are `not implemented` stubs). Entry
   strings must call the matching engine (`docker compose ...` vs
   `podman compose ...`).
3. **Dev lifecycle.** Confirmed canonical commands from §0:
   - boot deps/infra (e.g. `docker compose up --wait --build`)
   - first-time setup (install, codegen, migrations, seed)
   - foreground dev server (e.g. `npm run dev`, `docker compose logs -f`)
   - stop (e.g. `docker compose down`)
   - logs (e.g. `docker compose logs -f`)
   - reload, if the repo has one (e.g. `docker compose restart app`)
4. **Host ports the stack binds.** Every host-side port that must differ per
   worktree needs a `ports.base` entry **plus** a `ports.ranges` entry.
   Map each to the `.env` var the compose file actually reads (see port
   table below). Reuse the `ports.base` mapping proposed by the verdict.
   URL-shaped values with an explicit port belong in `urls:` (each needs
   `var`, `base` URL, `range`); each entry takes its own lowest free port
   in its `range`.
5. **Branch names** the user wants in parallel (for slug/validation context).

If anything is ambiguous (which compose file is canonical, what the setup
sequence is, which ports matter), ask — do not guess entry commands.

## 3. Author `wrk3.yaml`

Start from the template in `examples/compose-stack.yaml` (this skill dir) or
the `wrk3` repo's `wrk3.yaml.example`. Write the new config to a **local,
gitignored** path in the repo root (`wrk3.<name>.local.yaml`) unless the
user explicitly wants a committed template — the config must live in the
repo root (its directory is the repo root).

```yaml
project:
  worktreeBase: .worktrees           # relative => <repoRoot>/.worktrees
source:
  type: git                          # only backend that ships
  git: {remote: origin, fetchPrune: true}
runner:
  type: docker                       # docker or podman (portainer/nomad are stubs)
  docker:
    composeFiles: [docker-compose.yml]  # must exist in the repo
    projectPrefix: demo                 # optional; empty/missing => slug-only -p <slug>
entry:
  setup: ["docker compose up --wait --build"]  # verbatim repo commands, in order
  run: "docker compose logs -f"                # required
  stop: "docker compose down"                  # required
  logs: "docker compose logs -f"               # optional; used by logs
  reload: ["docker compose restart app"]       # optional; used by reload
ports:
  base: {app: 8000}                # every host port your stack binds (+ `app` required)
  ranges:
    app: [8000, 8099]              # per-service [min, max]; base must sit inside
# urls:                              # optional config-driven URLs (each needs var + base + range)
#   - {var: BASE_URL, base: "http://localhost:8000", range: [8000, 8099]}
# proxy: {enabled: false, domain: localhost, addr: 127.0.0.1:8080}  # optional gateway
# health:                            # optional display-only checks
#   checks:
#     - {name: api, run: "curl -sf http://localhost:${APP_PORT}/healthz", timeout: 10s}
```

Constraints (enforced by `config.Validate` — unknown values fail fast):

- `source.type` must be `git`. `runner.type` must be `docker` or `podman`
  for real runs (`portainer`/`nomad` are stubs).
- `runner.{docker,podman}.composeFiles` non-empty; `projectPrefix`
  optional (empty/missing/whitespace-only means slug-only project names,
  which can collide across repos sharing a daemon).
- `entry.run` and `entry.stop` non-empty. Each entry string runs via
  `sh -c` with `cwd=<worktree>`, `env=<allocated ports>`. `entry.reload`
  optional (empty means `reload` errors).
- `project.worktreeBase`: relative resolves against the repo root
  (the directory containing `wrk3.yaml`); absolute passes through.
- `ports.base`: `app` required; every key needs a `ranges` entry
  (`1–65535`, `min <= max`, `base` inside its range). Legacy `ports.step`
  is a hard error — delete it and add `ranges`. Two names may share one
  value as aliases (allocated once, stay equal); names colliding on the
  same `<NAME>_PORT` variable are rejected.
- `urls` (optional): each entry needs `var` (valid `.env` name, unique,
  must not collide with a managed `<NAME>_PORT` key), `base` (absolute
  URL with an explicit port), `range` (`[min,max]`, base port inside).
  Each entry allocates its own lowest free port in its `range`.
- `proxy` (optional): `addr` must be `host:port` with port `>= 1024`
  (privileged ports rejected); when `enabled`, `up`/`add`/dashboard ensure
  the gateway best-effort and runner env gains `APP_URL` (never written
  into the worktree `.env`).
- `health.checks` (optional, display-only): each needs unique non-empty
  `name` + `run` (via `sh -c`, `cwd=worktree`, `env=ports`); `timeout`
  default `10s`, must be `1s–120s` when set. Never fails `up`.

### Port mapping (`ports.base` → `.env` vars)

Only include names your stack actually binds; extra names are harmless.
`app` is required. Every name becomes `<NAME>_PORT` (uppercased,
non-alphanumerics → `_`): `app` → `APP_PORT`, `web` → `WEB_PORT`.

Managed `.env` keys are exactly: one `<NAME>_PORT` per `ports.base`
entry (aliases share one port and stay equal) **plus** every `urls[].var`.
On ensure wrk3 overwrites managed keys to the worktree allocation
(first assignment still wins for ordering; re-running is idempotent)
and appends missing managed keys under a `# Managed by wrk3` marker;
comments, blanks, secrets, and unmanaged lines pass through verbatim.
Fresh worktrees inherit non-managed keys from the repo-root `.env` seed
(managed port values never carry over). `APP_URL` from the gateway is
runner env only — never written into `.env` — and `remove` strips only
managed keys. `source.git.copy` must not name `.env` (`.env.local`
still allowed).

Three rules for parallel safety (all verified in §0):

1. The compose files **must** consume these vars for host-port bindings
   (e.g. `"${APP_PORT:-8000}:8000"`). If a compose file hardcodes a
   host port, either parameterize it first (§1) or accept the conflict and say so.
2. The compose files **must not** set `container_name:` — it is global on
   the daemon and collides across worktrees (`up` fails fast naming the
   file/services). Delete it (§1); compose generates `<project>-<service>-1`.
3. Two configs sharing one host need distinct `ports.base` offsets or
   non-overlapping `ports.ranges`, otherwise worktree 0 of project A collides with worktree 0 of project B.

## 4. Validate (read-only first)

```bash
wrk3 fetch    # git fetch --prune + list origin/* refs (run inside the repo)
wrk3 status   # renders empty table on a fresh config
# or from anywhere: wrk3 -f <path-to-yaml> fetch
```

Both must exit 0. Typical failures and fixes:

| Error | Fix |
| ----- | --- |
| `project.worktreeBase must not be empty` | Set `project.worktreeBase` (e.g. `.worktrees`). |
| `unknown source/runner type` | Must be `git` / `docker` or `podman`; check spelling. |
| `runner.docker.composeFiles must list at least one` | Add the compose file found in step 1. |
| `entry.run must not be empty` | Fill `run` and `stop` from repo docs. |
| `ports.step was removed` | Delete `ports.step`, add per-service `ports.ranges` (e.g. `ranges: {app: [8000, 8099]}`). |
| `ports.base "app" ...` / `ranges ...` | Add missing `app`, put every `base` inside its `range` (`1–65535`, `min <= max`). |
| `load config ... no such file` | Wrong `-f` path or no `wrk3.yaml`/`wrk3.yml` above cwd. |
| `sets container_name for service(s)` | §0 miss — delete `container_name:` from the named file/services. |
| `urls[...]` / `proxy.addr ... requires root` | Fix the `urls` entry (`var`/`base` URL with port/`range`) or use a `proxy.addr` port ≥ 1024. |

## 5. Live verification (needs user approval)

`add` creates real worktrees; `up` boots real containers. Confirm before
running, especially on heavy stacks (double `setup` can take minutes —
flag this when §0 noted large images or slow seeds).
Bare `add` opens the interactive branch picker; bare `up`/`down` apply
to all worktrees including the implicit main checkout (index `0` at
exactly the `ports.base` allocation).

```bash
wrk3 add <branch>            # worktree + ports + .env (or bare `add` to pick)
wrk3 up                      # setup → compose up → run (all worktrees, in parallel)
wrk3 status                  # expect running + distinct ports
wrk3 logs <branch> --follow  # entry.logs (or compose logs); `--follow` long form only (-f is --file)
wrk3 down
wrk3 remove --all            # compose down -v + worktree remove (never touches main)
```

For a second parallel instance, `add`/`up` another branch and confirm
`status` shows distinct ports and distinct `<prefix>-<slug>` compose projects.

## 6. Hand off

- Show the user the final config path, the §0 verdict, and `status` output.
- If the config is meant to be shared, copy it to `wrk3.yaml.example`
  shape — never commit personal local configs.
- Never commit `wrk3.<name>.local.yaml`, `.wrk3-state.json`,
  `.wrk3-log.jsonl*`, `.wrk3-proxy.{pid,log}`, `.worktrees/`, overlay
  dirs, or `.env` files.
