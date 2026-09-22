# Configuration reference (`wrk3.yaml`)

Start from [`wrk3.yaml.example`](../wrk3.yaml.example). Setting up a new
repo? Learn the developer onboarding first (manual setup flow, env files,
install → migrate/seed → run), ask the developer when unsure, and encode
it as `wrk3.yaml`. Validate any
config with `wrk3 -f <path> status` — config errors fail fast and
list the available `source.type` / `runner.type` options. The any-codebase
project goal and runner coverage live in [`ROADMAP.md`](../ROADMAP.md).

## Minimal example

```yaml
project:
  worktreeBase: .worktrees        # required; relative => resolved against repo root (config dir)
source:
  type: git
  git:
    remote: origin
    fetchPrune: true
    # copy: [".env.local", "certs/"]  # optional; repo-relative files/dirs into each new worktree on add
runner:
  type: docker
  docker:
    composeFiles: [docker-compose.yml]  # at least one
    projectPrefix: demo                 # optional; compose -p <prefix>-<slug>, empty/missing => -p <slug>
  # podman:                             # alternative backend (runner.type: podman)
  #   composeFiles: [docker-compose.yml]
  #   projectPrefix: demo
entry:
  setup: ["docker compose up --wait --build"]  # run in order before compose up (use `podman compose ...` with runner.type: podman)
  run: "docker compose logs -f"                # required; run after compose up
  stop: "docker compose down"                  # required; used by down
  logs: "docker compose logs -f"               # optional; used by logs
  reload: ["docker compose restart app"]       # optional; used by reload (errors when empty)
ports:
  base: {app: 8000}
  ranges:
    app: [8000, 8099]
# proxy: {enabled: false, domain: localhost, addr: 127.0.0.1:8080}  # optional gateway; see below
# health:  # optional health checks (display-only); see below
#   checks:
#     - {name: api, run: "curl -sf http://localhost:${APP_PORT}/healthz", timeout: 10s}
```

## Fields

| Path | Required | Notes |
| ---- | -------- | ----- |
| `project.worktreeBase` | yes | Directory holding worktrees + `.wrk3-state.json`. The repo root is the directory containing `wrk3.yaml` (the config always lives in the project root). Relative values resolve against the repo root; absolute values pass through. Worktrees are created with `git -C <repoRoot>`. |
| `source.type` | yes | `git` (only backend that ships). Unknown values error listing `[git]`. |
| `source.git.remote` | no | Default `origin`. Used by `fetch` (`fetch --prune`) and ref listing. `--remote <name>` on `fetch`/`add` overrides it per-invocation; `--mine` on `add` requires remote mode (uses the flag or this value). Remote-only branches are created as tracking branches (`--track -b`). New branches (`add <name>` for an unknown name, after confirm or `--create`) start from `<remote>/<default>` (`refs/remotes/<remote>/HEAD`, else `main`/`master` probe, else `HEAD` with a warning). |
| `source.git.fetchPrune` | no | Default `true`. |
| `source.git.copy` | no | Repo-relative files/dirs (globs allowed, `**` supported) copied from the repo root into each new worktree on `add` (e.g. `[".env.local", "certs/", "storage/*.sqlite"]`). Rules: non-empty, not absolute, no `..` escape, must not name `.env` (wrk3 manages that file). Missing sources skip with a warning; existing destinations are never overwritten (dirs merge). Applies to `add` only, not adopt. `.git` metadata and `.env` are never copied. Copied files stay gitignored — never commit worktree secrets. |

### PR filters (`--myprs`)

`fetch --myprs`, `add --myprs`, and `dashboard --myprs` (toggle `P`)
narrow the branch list to branches with an **open PR involving you**
(`is:pr state:open involves:@me`, author/reviewer/assignee/mentioned —
broader than `--mine`, which matches git commit authorship).

- **GitHub only, via `gh`.** The forge is detected from
  `git remote get-url <remote>` — no config needed. Non-GitHub remotes
  error naming the remote and the supported forges; missing/unauthenticated
  `gh` errors point at `https://cli.github.com` and `gh auth login`.
  wrk3 stores no tokens (auth lives in your `gh` session).
- **Intersection semantics.** `--myprs` applies after `--mine`/`--author`,
  so `fetch --myprs --author alice` means "my-PR branches also matching
  alice". Fork-head PRs with no `<remote>/<branch>` ref drop out instead
  of failing `add`.
- **Dashboard.** Each registered project resolves its own remote, so
  non-GitHub projects show the gate message in the branch pane while
  GitHub projects filter normally.

| Path | Required | Notes |
| ---- | -------- | ----- |
| `runner.type` | yes | `docker` or `podman` (both ship). `portainer`/`nomad` exist as stubs → `not implemented`. Unknown values error listing available runners. |
| `runner.docker.composeFiles` | yes (docker) | At least one compose file, resolved inside each worktree. Compose files must not set `container_name:` — it is global on the daemon and bypasses `-p <prefix>-<slug>` isolation, so `up` fails fast naming the offending file/services. Compose generates `<project>-<service>-1` automatically. |
| `runner.docker.projectPrefix` | no (docker) | Compose project is `<prefix>-<slug>` → free volume/network isolation. Empty, missing, or whitespace-only means slug-only (`<slug>`); slug-only names can collide across repos sharing a daemon. |
| `runner.podman.composeFiles` | yes (podman) | Same as `runner.docker.composeFiles`, for `podman compose`. `up` runs the same `container_name:` preflight. |
| `runner.podman.projectPrefix` | no (podman) | Same as `runner.docker.projectPrefix`, for `podman compose` (empty => slug-only). |
| `entry.setup` | no | Ordered list, each run via `sh -c` with `cwd=worktree`, `env=allocated ports`. Empty strings skipped. |
| `entry.run` | yes | Long-running command started after compose up (e.g. dev server). Run via `sh -c` with `cwd=worktree`, `env=allocated ports`. |
| `entry.stop` | yes | Run via `sh -c` before `compose down` by `down` (failures warn, never block teardown). |
| `entry.logs` | no | When set, `logs` runs it via `sh -c` instead of `compose logs`; when empty, `compose logs` is used. |
| `entry.reload` | no | Ordered list run by `reload` (CLI + dashboard `l`), each via `sh -c` with `cwd=worktree`, `env=allocated ports`. Empty strings skipped. `reload` errors when nothing is set. |
| `ports.base` | no | Defaults to `{app: 8000}`. `app` is required; add more names when the stack binds extra host ports. Each base must sit inside its `ports.ranges` entry. Two names may share one value as aliases for a single host port (e.g. `{app: 8000, public_api: 8000}` when both vars address one listener): aliases are allocated once and stay equal in every worktree. Names mapping to the same `<NAME>_PORT` variable (e.g. `api-v2` and `api_v2`) are rejected. |
| `ports.ranges` | no | Defaults to `{app: [8000, 8099]}`. Per-service `[min, max]` inclusive; `app` required; every `base` key needs a range (1–65535, `min <= max`). Legacy `ports.step` is a hard error: delete it and add `ranges` instead. |
| `urls` | no | Optional list of `{var, base, range}` URL vars rewritten per worktree (e.g. `- {var: BASE_URL, base: http://localhost:8000, range: [8000, 8099]}`). `var` must be a unique valid `.env` name that does not collide with a managed `<NAME>_PORT` key; `base` must be an absolute URL with an explicit port inside `range` (`[min, max]`, 1–65535). A URL group whose base port matches a `ports.base` value tracks that host service and reuses its port (e.g. `BASE_URL` `http://localhost:8000` renders the `app` listener, so it equals `APP_PORT` in every worktree). Other URL groups take the lowest free port from their base (gap reuse, `127.0.0.1` bind probe, main URL reservations held) and never share with another distinct group. Specs sharing one base port are aliases for a single URL (e.g. `BASE_URL` and `ALLOWED_WS_ORIGINS` both `http://localhost:8000`): they are allocated once and stay equal in every worktree. |
| `proxy.enabled` | no | Default `false`. When `true`, `up`/`add`/dashboard ensure the local gateway (best-effort, never fails the command) and runner env gains `APP_URL` (not written into the worktree `.env`). |
| `proxy.domain` | no | Default `localhost` → `http://<slug>.localhost:<port>`. Lowercased, hostname chars only. `.localhost` needs no setup in Chrome/Firefox/Edge (RFC 6761); Safari and non-browser clients need `wrk3 proxy hosts-sync`. Avoid `.local` (mDNS/Bonjour conflicts on macOS). |
| `proxy.addr` | no | Default `127.0.0.1:8080`. Gateway listen addr, must be `host:port` with port 1-65535 (`:80` needs root, so a high port is the default). |
| `health.checks` | no | Optional list of `{name, run, timeout}` probes (see below). Empty/missing means no shell checks; compose container health is still probed automatically. |
| `health.checks[].name` | yes (per check) | Non-empty, unique per config. Shown in the dashboard DETAILS pane (`api: pass`). |
| `health.checks[].run` | yes (per check) | Shell string run via `sh -c` with `cwd=worktree`, `env=allocated ports` (like `entry.*`). Exit 0 = pass. |
| `health.checks[].timeout` | no (per check) | Go duration string, default `10s`, must be `1s`–`120s` when set. |

## Health checks

Display-only Docker-style health for running worktrees. Each `health.checks`
entry plus every compose container with a `healthcheck:` becomes one check;
`status`/`ls`/dashboard append the aggregate to the `STATUS` cell:

- `running (healthy)` — all checks pass;
- `running (degraded 1/2)` — some pass (count shown);
- `running (unhealthy)` — none pass (a single failing check lands here);
- bare `running` — no checks configured and no container health reported.

Probes run live on every read (shell checks in parallel; the shell phase
budget is the longest per-check timeout plus 5s headroom, the container
phase gets 30s) and only for running worktrees —
`stopped`/`setting up`/`failed`/`stale` never gain a suffix. Failures never
fail `up`, never block `down`, and never persist to `.wrk3-state.json`.
The dashboard DETAILS preview lists the per-check breakdown
(`api: pass, db: fail: ...`); containers appear as `container:<name>`.
Like `entry.*`, `run` strings execute from your `wrk3.yaml` — only use
configs you trust.

Health checks are display-only by design, so they never gate `up` or
`down`. If you want `up` to wait until the API inside the container is
actually answering, gate readiness in the entry commands themselves:
the built-in compose step is `up -d --build`, which returns once
containers are *started* (not *healthy*). Use `entry.setup` with
`docker compose up --wait --build` (the example config's default) when
services declare a `healthcheck:`, or put a polling/retry loop in
`entry.run` (e.g.
`until curl -sf http://localhost:${APP_PORT}/healthz; do sleep 1; done`) —
wrk3 runs every entry string via `sh -c` in the worktree and waits for
it to exit before moving on.

## Ports and `.env`

- Each `add` keeps a monotonic index (`max(index)+1`, floored at 1 —
  index `0` is reserved for the repo-root main checkout) but allocates
  ports from ranges: per service scan `base, base+1, … ≤ max`, lowest
  port wins (e.g. `8001` with `base: {app: 8000}`,
  `ranges: {app: [8000, 8099]}` since main holds `8000`). Freed ports
  are reused (gap reuse); ports occupied by another process are skipped
  via a `127.0.0.1` bind probe at assign time; exhaustion fails with
  `no free port for "<svc>" in [min,max]`. Stale rows (dir missing,
  still in state) hold ports until `remove --force`. The allocation is
  ensured in the worktree's `.env` plus the state file.
  Ensure means: wrk3 writes the current allocation into managed
  `<NAME>_PORT` keys (rewriting the existing assignment line when the
  key is already present; first assignment still wins) and appends only
  the managed keys that are missing (under a `# Managed by wrk3` marker).
  Comments, blanks, and unmanaged lines — secrets, user URLs — pass
  through verbatim. Re-running is idempotent and never duplicates keys.
  Fresh worktrees with no `.env` get a generated one (`# Generated by wrk3`
  header), seeded with the repo-root `.env`'s non-managed keys (secrets
  are inherited; managed port values never are — each worktree keeps its
  own allocation).
- Every `add`/`up`/`down`/`exec` re-ensures the target worktree's `.env`,
  so a tracked or copied `.env` that leaked the main checkout's ports is
  rewritten to this worktree's allocation on next use. (`status`/`ls`
  stay read-only and never touch `.env`.)
- Managed keys: one `<NAME>_PORT` per `ports.base` entry. Names sharing one
  `ports.base` value are aliases for a single host port (e.g. `app` and
  `public_api` both `8000`): every worktree assigns them the same port so
  they stay in lockstep. Each configured `urls` entry is also managed
  (`<VAR>=<rewritten URL>` with its allocated port). A URL group whose
  base port matches a `ports.base` value tracks that host service and
  reuses its port (e.g. `BASE_URL` `http://localhost:8000` equals
  `APP_PORT`); other groups take the lowest free port. Entries sharing
  one base port are aliases for a single URL and stay equal (e.g.
  `BASE_URL` and `ALLOWED_WS_ORIGINS` both `http://localhost:8000`).
  Unconfigured app URLs such
  as a user-set `APP_URL` are never managed: they copy
  verbatim from the repo-root `.env` seed into fresh worktrees. When
  `proxy.enabled`, runner env still receives `APP_URL`
  (`http://<slug>.<domain>[:port]`); it is not written into the `.env`.
- The repo-root main checkout is implicit (no state entry) with reserved
  index `0`, i.e. exactly the `ports.base` allocation (e.g. `8000` with
  `base: {app: 8000}`). Its
  managed `.env` section is ensured on `up`/`down`/`exec` like any other
  worktree. Port/project collisions with managed worktrees surface as errors.
- `remove` strips only the managed keys from the worktree `.env` (deleting
  the file when nothing but managed keys remain), so your secrets survive
  worktree removal.
- `.env` variable mapping (`internal/ports`, `cmd/common.go`): every port
  name becomes `<NAME>_PORT` (uppercased, non-alphanumerics → `_`), sorted
  for stable output. Examples: `app` → `APP_PORT`, `web` → `WEB_PORT`.

`COMPOSE_PROJECT_NAME` is forced by the docker and podman runners (not set in `.env`).

Only use the port names your compose files read — extra names are
harmless. To adapt: change `ports.base` keys/values and make sure your
compose/`entry` commands consume the matching `.env` vars (e.g.
`"${APP_PORT:-8000}:8000"`). How recovered ports, reconcile, and live
docker status sync on every read: [USAGE.md](USAGE.md).

## Local gateway (`proxy`)

Opt-in stdlib reverse proxy (no Caddy/binary dependency) mapping
`http://<slug>.<domain>[:port]` → `127.0.0.1:<appPort>` per worktree
(including the implicit main checkout). The gateway resolves the `Host`
header per request against `<worktreeBase>/.wrk3-state.json`, so
`add`/`up`/`down`/`remove` take effect immediately with no route sync.

- `up`, `add`, and the dashboard ensure the gateway in the background when `proxy.enabled`
  (never fails the command: spawn errors warn and worktrees stay reachable via
  `localhost` ports). Manage it explicitly with `wrk3 proxy
  up|down|status|open <worktree>|hosts-sync` (see `USAGE.md`).
- `status` grows a `URL` column when enabled; the dashboard always shows a
  `URL` column (gateway URL when enabled, else `localhost:<appPort>`, opened
  with `o`) plus gateway state in its meta line; runner env gains `APP_URL`
  when the gateway is enabled (`remove` does not strip a user-owned `APP_URL`
  from the `.env`).
- DNS: `.localhost` subdomains auto-resolve in Chrome/Firefox/Edge.
  Safari, `curl`, and server-side clients need real resolution — run
  `sudo wrk3 proxy hosts-sync` to append `127.0.0.1 <slug>.localhost`
  lines (idempotent, under a `# Managed by wrk3 proxy` marker), or set up
  `dnsmasq` + `/etc/resolver/localhost` for a wildcard.
- HTTP only (no local TLS in v1); unknown slugs answer `404`, unreachable
  backends `502`. Pid/log live next to state:
  `<worktreeBase>/.wrk3-proxy.{pid,log}`.

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

## User preferences file

Global preferences live in `~/.config/wrk3/preferences.yaml` (XDG-aware,
`$WRK3_CONFIG_HOME` override, same path resolution as the project registry).
Current use:

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `telemetry.enabled` | bool | false | Anonymous error reporting opt-in |
| `telemetry.prompted` | bool | false | Whether the first-run dashboard prompt has been shown |

Managed by `wrk3 telemetry enable|disable|status`. Missing or empty file
means both fields are false.

## Local configs

Start from `wrk3.yaml.example` (committed template). Copy it to `wrk3.yaml`
in the repo root for real use — `wrk3.yaml` itself is typically gitignored
in app repos; commit only shareable templates like `wrk3.yaml.example`.
Name personal variants `wrk3.<name>.local.yaml` — gitignored. The config
directory is the repo root, so no `repo` path is stored.
