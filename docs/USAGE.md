# Usage

Every command finds its config like `docker compose`: `-f/--file <path>`
wins, otherwise wrk3 walks up from cwd looking for `wrk3.yaml`, then
`wrk3.yml` (nearest directory wins). Run from anywhere inside the repo;
use `-f` from outside it.

```bash
wrk3 -f ./wrk3.yaml status
```

Shell completion (static commands/flags + dynamic branches/worktrees):

```bash
wrk3 completion bash > ~/.local/share/bash-completion/completions/wrk3
wrk3 completion zsh > "${fpath[1]}/_wrk3"
wrk3 completion fish > ~/.config/fish/completions/wrk3.fish
wrk3 completion powershell | Out-String | Invoke-Expression
```

`add` completes remote branches (for the effective remote — `--remote`
flag > `source.git.remote` > `origin`); `up`/`down`/`reload`/`pull`/`logs`/`exec`/`env`/`checkout`/`git-status`/`proxy open`
complete existing worktrees (branch names and slugs, prefix-filtered on
what you typed — already-typed names are not re-suggested);
`remove` completes worktrees except the implicit main checkout (which
`remove` refuses); `ls --project` completes registry project names.
`exec` completes the worktree only in first position — after that the
inner command keeps normal file/command completion. Commands taking no
positional args (`status`, `fetch`, `log`, `ls`, `dashboard`,
`version`, `update`, `project`, `proxy up/down/status/hosts-sync`)
suppress file completion so TAB only offers flags. Completion never
fetches from the network — it uses the last `fetch` results.

## Switching branches/worktrees

Worktrees can live in nested or otherwise awkward paths, so `checkout`
jumps straight to the folder — branch names and slugs work
interchangeably, including the implicit main checkout:

```bash
eval "$(wrk3 shell-init bash)"            # once, in ~/.bashrc (zsh/fish/powershell too)
wrk3 checkout feature-a                  # cd to the worktree (co/switch aliases work too)
wrk3 checkout feat/new-feature           # same by branch name
wrk3 checkout --print feature-a          # script-safe: print path, never cd
cd "$(wrk3 checkout feature-a)"          # same, without shell integration
```

A binary cannot `cd` its parent shell, so `checkout` needs the wrapper
above to change directories (worktrunk-style directive file: wrk3 writes
the raw path to `$WRK3_DIRECTIVE_CD_FILE`, the wrapper `cd`s after wrk3
exits). Without it, `checkout` prints the path instead. Stale entries
(missing directory) and unknown names are errors, not silent no-ops.

## Daily loop

```bash
wrk3 fetch                          # git fetch --prune, list origin/* refs
wrk3 fetch --remote upstream        # same against upstream (default: source.git.remote, else origin)
wrk3 fetch --mine                   # only your branches: tip or last 100 branch-exclusive commits match git config user.name/user.email (covers bot/cursor tips you pushed)
wrk3 fetch --author alice           # substring match on author/committer name/email in tip or history (case-insensitive; repeatable, comma-split)
wrk3 fetch --myprs                  # only branches with an open PR involving you (GitHub remotes only, via gh; like pulls?q=is:pr+state:open+involves:@me)
wrk3 add feature-a feature-b        # worktree add + ports + .env ensure
wrk3 add                            # bare = interactive picker over remote branches
wrk3 add --remote upstream --mine   # fetch upstream, create all my branches (skip registered/checked-out)
wrk3 add --myprs                    # fetch effective remote, create all branches with open PRs involving you (GitHub, via gh)
wrk3 add --remote upstream --myprs  # same against upstream; combine with --mine to intersect both filters
wrk3 add --remote upstream          # fetch upstream, create all its branches
wrk3 add --remote upstream hotfix   # create one branch, tracking upstream when remote-only
wrk3 add --local                    # adopt all existing local worktrees (no fetch from origin)
wrk3 add --local feature-a          # adopt only the named local worktree(s)
wrk3 add feature-a                  # adopts the on-disk worktree when one exists, else creates it
wrk3 add feat/new-feature           # unknown name: refresh remote, then prompt to create from origin/<default> ([y/N])
wrk3 add feat/new-feature --create  # same, without prompting (for scripts: no prompt, no hang on EOF)
wrk3 add feat/new-feature --no-create # fail fast on unknown names instead of prompting
wrk3 up feature-a                   # setup entries + compose up + run entry
wrk3 up                             # bare = all worktrees including main, in parallel (each worktree runs to completion; a failure marks that worktree failed without aborting the others)
wrk3 reload feature-a | wrk3 reload # entry.reload commands (bare = all including main, in parallel)
wrk3 pull feature-a | wrk3 pull     # git pull in one worktree (bare = all including main, in parallel; --rebase/--ff-only)
wrk3 git-status | wrk3 git-status feature-a  # git status per worktree (bare = all including main, in parallel; --short adds the file list; gs alias)
wrk3 status                         # this config (full table, includes main)
wrk3 ls                             # this config (minimal WORKTREE/BRANCH/STATUS/PORTS, includes main)
wrk3 ls --project myapp             # worktrees in a registered project, from anywhere
wrk3 project ls                     # all auto-registered projects (NAME/CONFIG/WORKTREES)
wrk3 logs feature-a [--follow]     # entry.logs command
wrk3 log [-n 50] [--json]           # persistent system log: every command run, state change, warning/error (next to the state file)
wrk3 exec feature-a -- <cmd...>     # run inside worktree env (cwd=worktree)
wrk3 env feature-a                 # open the worktree .env in $VISUAL/$EDITOR (ensured first; managed ports overwritten)
wrk3 env feature-a --print         # print the worktree .env to stdout instead
wrk3 checkout feature-a             # cd to the worktree (needs shell-init wrapper; else prints path)
wrk3 down feature-a | wrk3 down     # bare = all including main
wrk3 remove feature-a feature-b | wrk3 remove --all   # compose down -v + worktree remove + state cleanup (never touches main)
wrk3 remove --force feature-a | wrk3 remove --all --force  # same with git worktree remove --force (falls back to rm -rf); for dirty worktrees with modified/untracked files
wrk3 dashboard                       # interactive TUI: worktrees + branches + ports, up/down/add/remove (shorthand: wrk3 db)
wrk3 dashboard --project myapp       # same, starting from a registered project
wrk3 update --check                 # show latest release without installing
wrk3 update                         # install latest over the current binary (sha256 verified)
wrk3 proxy status                   # gateway state + per-worktree http://<slug>.localhost URLs
wrk3 proxy up | wrk3 proxy down     # start/stop the gateway (auto-started by up/add/dashboard when proxy.enabled)
wrk3 proxy open feature-a           # open the worktree URL in a browser
sudo wrk3 proxy hosts-sync          # 127.0.0.1 entries for Safari/curl (Chrome/FF/Edge need nothing)
```

Copy includes: `source.git.copy` lists repo-relative files/dirs (globs,
`**` supported) copied from the repo root into each new worktree on `add`
— use it for gitignored files like `.env.local`, `certs/`, or
`storage/*.sqlite`. Listing `.env` itself is rejected (wrk3 manages that
file and will not copy it). Missing sources skip with a warning and existing files
are never overwritten (dirs merge); `add --local` adoption never copies.

Branch slugs: `feature/foo` → `feature-foo` (max 50 chars). `add`/`up`/`down`
accept branch names or slugs interchangeably; `status` shows
`WORKTREE/BRANCH/STATUS/PORTS/COMPOSE_PROJECT` (plus `URL` when
`proxy.enabled`), with `?` and
`stale` when a worktree directory is missing. `PORTS` lists every
allocated port as `name=value` (`app` first, rest alphabetical,
e.g. `app=8000,web=3000`).
`up` marks targets `setting up` at start, then `running` on success or
`failed` on error — so the table never claims `running` mid-setup. Live
`running` always wins over stored state (including `setting up` /
`stopping` / `failed`), so a stack started out-of-band clears stale
status; otherwise transitional states are kept over a non-running probe
while entries execute. A failed `up` stays `failed` until the next
`up`/`down` or until containers are actually observed running. `down`
marks targets `stopping` at start, then `stopped` on success; failures
keep `stopping` until the next `down` fixes them.

Git status: `wrk3 git-status` (`gs` alias) checks `git status` of all
local worktrees and displays one row per worktree —
`WORKTREE/BRANCH/GIT/AHEAD/BEHIND/STAGED/UNSTAGED/UNTRACKED` (`GIT` is
`clean`, `dirty`, or `error` when the probe fails; `AHEAD`/`BEHIND`
count commits vs the upstream, `0` when there is none). Bare args mean
all worktrees including main (like `pull`/`up`/`down`); branch names and
slugs filter interchangeably. Probes run in parallel, stale entries
(missing directory) render as `error` and join into the exit error, and
`--short` also prints the short file list per dirty worktree.

Health (optional, display-only): configure `health.checks` (each `run`
via `sh -c` with `cwd=worktree`, `env=allocated ports`) and a running
worktree's `STATUS` gains a Docker-style suffix — `running (healthy)`
(all pass), `running (degraded 1/2)` (some pass), `running (unhealthy)`
(none pass); bare `running` when nothing reports. Compose containers with
a `healthcheck:` are included automatically. Health never fails `up`,
never blocks `down`, and never persists to the state file; only running
worktrees are probed. The dashboard DETAILS preview lists the per-check
breakdown (`api: pass, db: fail: ...`, containers as `container:<name>`).

### Concurrency and readiness

`up`, `down`, `reload`, `pull` and `git-status` run their worktrees
concurrently — one worker per worktree (`sync.WaitGroup`), so a slow
`setup` on one branch never blocks another branch's `up`. Within one
worktree the steps are strictly sequential: each `entry.setup` command,
then compose up, then the `entry.run` command, and wrk3 waits for
**every** command to exit before moving to the next or finishing the
worktree (bounded by the runner's per-invocation timeout). In other
words, `up` returns only when every worktree's commands have actually
finished — a `run` entry that polls an API
(`until curl -sf http://localhost:${APP_PORT}/healthz; do sleep 1; done`)
blocks `up` until it passes.

One nuance to know: the built-in compose step is
`docker compose up -d --build`, which returns once containers are
*started*, not once they are *healthy*. If your services declare a
`healthcheck:`, run the compose step yourself with `--wait` in
`entry.setup` (`docker compose up --wait --build`, the example config's
default) or gate readiness in `entry.run` — configured `health.checks`
are display-only and never fail or block `up` (see above).

Main checkout: the repo root is always included implicitly (no state entry)
in `up`/`down` (bare = all including main), `status`/`ls`/`git-status`, and as an
`exec`/`logs` target by branch/slug. It uses reserved port index `0`
(the `ports.base` allocation, e.g. `8000` with defaults) with the managed `.env` section ensured on `up`/`down`/`exec`;
`remove` refuses main and `remove --all` covers only managed worktrees.

State recovery: the state file (`.wrk3-state.json` under `worktreeBase`)
is a cache, not the truth. Every read (`status`, `ls`, `up`, `down`,
dashboard) reconciles it against `git worktree list` and syncs runtime
status back into it (writes happen only when something changed; CLI logs
`reconciled state: ...` / `synced runtime state: ...` on stderr, the
dashboard shows them in its log pane). Deleting the state file therefore
rebuilds it on next use. Branches with no checkout never enter state —
`add <branch>` (local or remote ref) and the dashboard create them. A name
matching neither offers to create a new branch from the remote default
(`--create`/`--no-create` to skip the prompt).

System log: every operation also appends to `.wrk3-log.jsonl` next to the
state file (JSONL, one entry per line: timestamp, op, branch, message,
exact command, cwd, duration, error). Entry strings (`sh -c`), compose
up/down, and git fetch/pull/worktree add/remove log always; read-only
probes (runner status, git list/refs) log only on error so background
polling stays quiet. The dashboard log pane stays brief (last 200
in-memory lines); `wrk3 log` shows the full persisted history
(`-n/--tail N`, `--json` for raw JSONL). The file rotates at 5 MiB to
`.wrk3-log.jsonl.1`, and reads span the backup so history stays
continuous.

### Worktree reconcile

On-disk worktrees missing from state (orphans from a deleted state file
or out-of-band `git worktree add`) are adopted automatically, in sorted
branch order for deterministic ports:

- Ports are recovered from the worktree `.env` only when the set matches
  the configured `ports.base` keys, sits inside `ports.ranges`, and
  collides with neither state (main included) nor the OS (bind probe);
  otherwise the lowest free range allocation is assigned (gap reuse,
  OS-aware, so a `ports.base`/`ranges` change never reuses a taken port).
  The worktree
  `.env` is ensured (managed port keys overwritten to the allocation).
  Adopted records start as `stopped` — display overlays the live
  probe.
- Slug or compose-project collisions are hard errors (state is never
  half-written).
- A `git worktree list` failure degrades to no adoption (offline-safe)
  rather than failing the read.

### Runtime sync + display

Every read probes the configured runner backend (docker or podman compose,
other orchestrators via the same Runner interface tomorrow) in parallel
(30s timeout per worktree). Missing directories short-circuit to
`stale`/`?` with no runner call; probe errors surface as `unknown`. The
implicit main checkout has no state entry — only managed records sync,
main is synthesized afterwards for display.

Persist rules (`running`/`stopped` drift is written back to the state
file): live `running` always persists (even over `setting up` /
`stopping` / `failed`, so an out-of-band `up` clears stale status);
live `stopped` only clears `running` / `failed` (a transient empty probe
mid-`up` never clobbers an in-progress transitional state); `unknown`
never persists (e.g. daemon unreachable).

Display rules (same for `status` / `ls` / dashboard): live `running`
always wins; otherwise stored `setting up` / `stopping` / `failed` win
over a non-running probe while entries execute; a probe error falls back
to stored state (`unknown` when nothing is stored).

Docker probe order: primary `docker compose -p <prefix>-<slug> ps -q` (`-p <slug>` when
projectPrefix is empty),
then label-based `docker ps --filter label=com.docker.compose.project=…`
fallbacks covering stacks started out-of-band via plain
`docker compose up` — slug, worktree folder basename, and stored project
(label check is independent of compose files and cwd). Any candidate with
>0 containers counts as `running`, else `stopped`.

Podman probe order: primary `podman compose -p <prefix>-<slug> ps -q` (`-p <slug>` when
projectPrefix is empty),
then label-based `podman ps` fallbacks over the same project candidates.
Both `com.docker.compose.project` (docker-compat) and
`io.podman.compose.project` (native) label keys are probed.

## Dashboard (TUI)

`wrk3 dashboard` (shorthand `wrk3 db`) opens an interactive view over the current repo plus every
registered project (`tab` switches projects). It polls worktree state and
remote branches (default every 15s, `--poll 0` disables), shows the slim worktree
list (SLUG + health STATUS with `x of y` counts), full branch/ports/URL
details in the DETAILS preview (clickable `URL`: gateway URL when `proxy.enabled`,
else `localhost:<appPort>`), project switcher + gateway state, and the next free `app` port,
and runs the same operations
as the CLI: `space` selects, `u`/`d`/`l`/`p` up/down/reload/pull (cursor worktree when nothing
is selected; `p` runs plain `git pull` like `wrk3 pull` with no flags), `a` adds queued branches, `o` opens the cursor worktree URL
in a browser (the full URL is logged too), `O` copies that URL to the OS
clipboard (`pbcopy` on macOS, `wl-copy`/`xclip`/`xsel` on Linux, `clip` on
Windows), `e` edits the cursor worktree `.env` in your editor
(`$VISUAL`, then `$EDITOR`, then nvim/vim/nano/vi; the TUI suspends
fullscreen while the editor runs and resumes on quit, the `.env` is
ensured first like `wrk3 env`), `x` removes (asks `y/n`, refuses
main like `remove`), `X` force-removes like `remove --force` (asks
`y/n`, for dirty worktrees with modified/untracked files), `r` refreshes
state, `R` fetches the remote
(`--remote`/`--mine`/`--author`/`--myprs` filter the branch list, `m`
toggles mine, `P` toggles myprs), `1`/`2`/`3` or `←`/`→` switch panes
(worktrees/branches/log; the DETAILS preview follows the worktree
cursor/selection and is never focused), `?` opens the lazydocker-style
`Menu` popup listing every action (`j/k`/`↑`/`↓` move, `enter` runs the
highlighted row, `esc` closes; `x`/`X` rows land in the usual `y/n`
confirm), `q` quits. The unfiltered branch pane unions remote refs
with local-only branches (filtered views stay remote-only); like
`add <branch>`, `a` creates the checkout from either ref, or adopts the
on-disk worktree when one already exists. The branch pane sorts your
branches first — open involving-me PR branches when the forge answers
(GitHub + `gh`, else tip-matching `--mine` branches) — then everything
else newest-first by tip committer date (git exposes no true branch
creation date, so tip recency is the proxy; local-only branches without
a remote ref sort last alphabetically). Filtered views (`--mine`/
`--author`/`--myprs`, `m`/`P` toggles) already are the priority set, so
they just sort newest-first. Layout is a lazygit-style 5-box grid
(two columns on terminals ≥80 cols, stacked otherwise) with `[n]` numbers
and `x of y` counts: [1]-Worktrees (top-left, slim SLUG + health STATUS
only), [2]-Branches (middle-left), Projects + status (bottom-left),
Details preview (top-right), and a large focusable [3]-Logs
(bottom-right, `j/k`/`↑`/`↓` scroll it when focused;
`pgup`/`pgdn`/`home`/`end` scroll from any pane),
the shortcut bar is always visible at the bottom.
Pressing `u` flips the selected rows to `setting up` immediately; the
rows keep that status (not `running`) until setup/run entries finish,
even when the setup itself already started containers. Pressing `l`
flips the selected rows to `setting up` the same way until the
`entry.reload` commands finish (and errors when `entry.reload` is empty).
Pressing `d` flips the selected rows to `stopping` immediately until
compose down finishes. Ops run in the background without freezing the
UI: navigation, selection (`space`), refresh (`r`), fetch (`R`), and
ops on unrelated worktrees stay live while an op runs (the header shows
e.g. `up feature-a +1 more…`). Only an op targeting the same branch as
a still-running op is rejected (`up blocked: … already running`),
and polling keeps refreshing in the background. Branches with an on-disk worktree missing from state show as
`orphan` and are queueable: `a` adopts them (ports + `.env` + state)
instead of re-creating the checkout.

The CLI keeps working alongside it: `wrk3 add` in another terminal shows
up on the next poll or manual refresh.

## Examples

```bash
# Two-review-stack flow
wrk3 add pr-101 pr-102
wrk3 up
wrk3 status
# WORKTREE  BRANCH  STATUS   PORTS       COMPOSE_PROJECT
# main      main    running  app=8000    demo-main
# pr-101    pr-101  running  app=8001    demo-pr-101
# pr-102    pr-102  running  app=8002    demo-pr-102

# Run tests inside one worktree without cd'ing there
wrk3 exec pr-101 -- go test ./... -count=1

# Clean up a single stack (volumes included via compose down -v)
wrk3 down pr-102 && wrk3 remove pr-102
```

## E2E

End-to-end coverage lives in `tests/e2e/` (`//go:build e2e`, case
catalog in `tests/e2e/CASES.md`):

```bash
make e2e            # no docker needed
WRK3_E2E_DOCKER=1 make e2e-docker  # real-docker add/up/status/down on throwaway repos
```

Each test builds a throwaway git repo in a temp dir, writes a minimal
`wrk3.yaml`, then drives `fetch`/`add`/`status`/`up`/`logs`/`down`/
`remove`/`exec` through the built binary. Docker tests use an
`alpine:3.19` `sleep infinity` stack and skip unless
`WRK3_E2E_DOCKER=1` with a reachable daemon.

## Troubleshooting

| Symptom | Likely cause / fix |
| ------- | ------------------ |
| `unknown source type "x" (available sources: [git])` | Typo in `source.type`; only `git` ships in v1. |
| `unknown runner type "x" (available runners: [docker podman ...])` | Only `docker` and `podman` are implemented; stubs return `not implemented`. |
| `project.worktreeBase must not be empty` | `project.worktreeBase` is required. |
| `no wrk3.yaml found ...` | Not inside a repo checkout, or config named differently — `cd` in or pass `-f <path>`. |
| `unknown worktree "foo"` | Name/slug not in state — check `wrk3 status`. |
| `stale` / `?` in status | Worktree directory deleted out-of-band; `remove --force` to clean state, or re-`add`. |
| deleted `.wrk3-state.json` | Self-heals: next `status`/`ls`/`up`/`down`/dashboard run re-adopts on-disk worktrees (ports from `.env` when intact). |
| `already checked out at ... (use add --local ...)` | On-disk worktree missing from state (e.g. state file deleted); `add <branch>` adopts it automatically, or use `add --local`. |
| `main worktree ports collide with worktree "x" ...` | Legacy guard only: current builds auto-migrate a managed allocation overlapping main's `ports.base` ports to the lowest free range allocation on the next `status`/`pull`/`up`/`down`/dashboard run — no manual `remove`+re-`add` needed. |
| `no free port for "<svc>" in [min,max]` | Range exhausted: `remove` a worktree to free a port or widen `ports.ranges` for that service. |
| `ports.step was removed ...` | Delete `ports.step` and add per-service `ports.ranges` (e.g. `ranges: {app: [8000, 8099]}`); ports now increment by 1 with gap reuse. |
| `pass either branch names or --all, not both` | `remove` takes explicit names **or** `--all`. |
| `--myprs supports GitHub remotes only ...` | The remote URL (`git remote get-url`) is not GitHub — `--myprs` is GitHub-only for now. |
| `github forge needs the gh CLI ...` / `gh is not authenticated ...` | Install `gh` from https://cli.github.com, then run `gh auth login` (wrk3 reuses your session, stores no tokens). |
| Port conflicts | Two checkouts sharing `base`+`ranges` on one host — give each config a distinct `ports.base` offset or non-overlapping `ports.ranges`. |
| `sets container_name for service(s) ...` | Compose file pins `container_name:`, which is global and collides across worktrees — delete it (compose generates `<project>-<service>-1`). |
| `Conflict. The container name ... is already in use` | Same cause as above on a stack that predates the preflight check — remove `container_name:` and `docker rm -f` the leftover, then `up` again. |

## Telemetry

`wrk3` includes opt-in anonymous error reporting via Sentry. Reporting is
off by default and is never enabled without explicit consent. Enabled
reports include a scrubbed stacktrace (module, function, line; no locals,
source context, paths, or personal data) so issues can group by location.

**First run:** the dashboard shows a one-time prompt (default No). `y`
enables; `n`/`esc` leaves off. Both states persist.

**CLI commands:**
- `wrk3 telemetry enable` — turn on reporting (prints what is/is not sent)
- `wrk3 telemetry disable` — turn off reporting
- `wrk3 telemetry status` — show current configuration

**Environment variables:**
- `WRK3_NO_TELEMETRY=1` — hard kill-switch (wins over the prefs file)
- `WRK3_SENTRY_DSN` — override the built-in Sentry DSN (forks/self-builds)
