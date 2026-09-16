# Configuration reference (`wrk3.yaml`)

Start from [`wrk3.yaml.example`](../wrk3.yaml.example). Setting up a new
repo? Run `wrk3 skill` first: the agent guide learns the developer
onboarding (manual setup flow, env files, install → migrate/seed → run),
asks the developer when unsure, and encodes it as `wrk3.yaml`. Validate any
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
    projectPrefix: demo                 # used as compose -p <prefix>-<slug>
entry:
  setup: ["docker compose up --wait --build"]  # run in order before compose up
  run: "docker compose logs -f"                # required; run after compose up
  stop: "docker compose down"                  # required; used by down
  logs: "docker compose logs -f"               # optional; used by logs
  reload: ["docker compose restart app"]       # optional; used by reload (errors when empty)
ports:
  base: {app: 8000}
  step: 100
# proxy: {enabled: false, domain: localhost, addr: 127.0.0.1:8080}  # optional gateway; see below
```

## Fields

| Path | Required | Notes |
| ---- | -------- | ----- |
| `project.worktreeBase` | yes | Directory holding worktrees + `.wrk3-state.json`. The repo root is the directory containing `wrk3.yaml` (the config always lives in the project root). Relative values resolve against the repo root; absolute values pass through. Worktrees are created with `git -C <repoRoot>`. |
| `source.type` | yes | `git` (only backend that ships). Unknown values error listing `[git]`. |
| `source.git.remote` | no | Default `origin`. Used by `fetch` (`fetch --prune`) and ref listing. `--remote <name>` on `fetch`/`add` overrides it per-invocation; `--mine` on `add` requires remote mode (uses the flag or this value). Remote-only branches are created as tracking branches (`--track -b`). New branches (`add <name>` for an unknown name, after confirm or `--create`) start from `<remote>/<default>` (`refs/remotes/<remote>/HEAD`, else `main`/`master` probe, else `HEAD` with a warning). |
| `source.git.fetchPrune` | no | Default `true`. |
| `source.git.copy` | no | Repo-relative files/dirs (globs allowed, `**` supported) copied from the repo root into each new worktree on `add` (e.g. `[".env.local", "certs/", "storage/*.sqlite"]`). Rules: non-empty, not absolute, no `..` escape. Missing sources skip with a warning; existing destinations are never overwritten (dirs merge). Applies to `add` only, not adopt. `.git` metadata is never copied. Copied files stay gitignored — never commit worktree secrets. |

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
| `runner.type` | yes | `docker` (ships). `portainer`/`nomad` exist as stubs → `not implemented`. Unknown values error listing available runners. |
| `runner.docker.composeFiles` | yes (docker) | At least one compose file, resolved inside each worktree. Compose files must not set `container_name:` — it is global on the daemon and bypasses `-p <prefix>-<slug>` isolation, so `up` fails fast naming the offending file/services. Compose generates `<project>-<service>-1` automatically. |
| `runner.docker.projectPrefix` | yes (docker) | Compose project becomes `<prefix>-<slug>` → free volume/network isolation. |
| `entry.setup` | no | Ordered list, each run via `sh -c` with `cwd=worktree`, `env=allocated ports`. Empty strings skipped. |
| `entry.run` | yes | Long-running command started after compose up (e.g. dev server). Run via `sh -c` with `cwd=worktree`, `env=allocated ports`. |
| `entry.stop` | yes | Run via `sh -c` before `compose down` by `down` (failures warn, never block teardown). |
| `entry.logs` | no | When set, `logs` runs it via `sh -c` instead of `compose logs`; when empty, `compose logs` is used. |
| `entry.reload` | no | Ordered list run by `reload` (CLI + dashboard `l`), each via `sh -c` with `cwd=worktree`, `env=allocated ports`. Empty strings skipped. `reload` errors when nothing is set. |
| `ports.base` | no | Defaults to `{app: 8000}`. `app` is required; add more names when the stack binds extra host ports. |
| `ports.step` | no | Default `100`. Allocation: `allocated[name] = base[name] + index*step`. |
| `proxy.enabled` | no | Default `false`. When `true`, `up`/`add`/dashboard ensure the local gateway (best-effort, never fails the command) and each worktree gains an append-only `APP_URL` in its `.env`. |
| `proxy.domain` | no | Default `localhost` → `http://<slug>.localhost:<port>`. Lowercased, hostname chars only. `.localhost` needs no setup in Chrome/Firefox/Edge (RFC 6761); Safari and non-browser clients need `wrk3 proxy hosts-sync`. Avoid `.local` (mDNS/Bonjour conflicts on macOS). |
| `proxy.addr` | no | Default `127.0.0.1:8080`. Gateway listen addr, must be `host:port` with port 1-65535 (`:80` needs root, so a high port is the default). |

## Ports and `.env`

- Each `add` takes the next index (`max(index)+1`, floored at 1 —
  index `0` is reserved for the repo-root main checkout, so the first
  managed worktree allocates index `1`, e.g. `8100` with
  `base: {app: 8000}, step: 100`) and
  ensures the allocation in the worktree's `.env` plus the state file.
  Ensure means: wrk3 checks the file has its managed port section and
  appends only the managed keys that are missing (under a
  `# Managed by wrk3` marker). Existing lines are never modified,
  overwritten, or deleted — secrets, comments, blank lines, and even
  already-set managed keys pass through verbatim. Re-running is idempotent
  and never duplicates keys. Fresh worktrees with no `.env` get a generated
  one (`# Generated by wrk3` header), seeded with the repo-root `.env`'s
  non-managed keys (secrets are inherited; managed port values never are —
  each worktree keeps its own allocation).
- A managed key already set to a different value is left intact and
  reported as a warning (`.env for "branch" already sets APP_PORT=...;
  leaving intact`), so a hand-edited port is never silently reverted.
- Every `add`/`up`/`down`/`exec` re-ensures the target worktree's `.env`,
  so manually created or repaired checkouts gain the managed section on
  next use. (`status`/`ls` stay read-only and never touch `.env`.)
- Managed keys: one `<NAME>_PORT` per `ports.base` entry, plus `APP_URL`
  (only when `proxy.enabled`: `http://<slug>.<domain>[:port]`; append-only,
  stripped on `remove`). App URLs such as `BASE_URL` are never managed:
  they copy verbatim from the repo-root `.env` seed into fresh worktrees.
- The repo-root main checkout is implicit (no state entry) with reserved
  index `0`, i.e. exactly the `ports.base` allocation (e.g. `8000` with
  `base: {app: 8000}, step: 100`). Its
  managed `.env` section is ensured on `up`/`down`/`exec` like any other
  worktree. Port/project collisions with managed worktrees surface as errors.
- `remove` strips only the managed keys from the worktree `.env` (deleting
  the file when nothing but managed keys remain), so your secrets survive
  worktree removal.
- `.env` variable mapping (`internal/ports`, `cmd/common.go`): every port
  name becomes `<NAME>_PORT` (uppercased, non-alphanumerics → `_`), sorted
  for stable output. Examples: `app` → `APP_PORT`, `web` → `WEB_PORT`.

`COMPOSE_PROJECT_NAME` is forced by the docker runner (not set in `.env`).

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
  with `o`) plus gateway state in its meta line; every worktree `.env` and
  runner env gains append-only `APP_URL` (existing values never
  overwritten; `remove` strips it with the other managed keys).
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

## Local configs

Start from `wrk3.yaml.example` (committed template). Copy it to `wrk3.yaml`
in the repo root for real use — `wrk3.yaml` itself is typically gitignored
in app repos; commit only shareable templates like `wrk3.yaml.example`.
Name personal variants `wrk3.<name>.local.yaml` — gitignored. The config
directory is the repo root, so no `repo` path is stored.
