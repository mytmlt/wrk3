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
flag > `source.git.remote` > `origin`); `up`/`down`/`logs`/`exec`/`remove`
complete existing worktrees (branch names and slugs); `ls --project`
completes registry project names. Completion never
fetches from the network — it uses the last `fetch` results.

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
wrk3 up feature-a                   # setup entries + compose up + run entry
wrk3 up                             # bare = all worktrees including main, in parallel (errgroup)
wrk3 status                         # this config (full table, includes main)
wrk3 ls                             # this config (minimal WORKTREE/BRANCH/STATUS/PORTS, includes main)
wrk3 ls --project myapp             # worktrees in a registered project, from anywhere
wrk3 project ls                     # all auto-registered projects (NAME/CONFIG/WORKTREES)
wrk3 logs feature-a [-f]            # entry.logs command
wrk3 exec feature-a -- <cmd...>     # run inside worktree env (cwd=worktree)
wrk3 down feature-a | wrk3 down     # bare = all including main
wrk3 remove feature-a feature-b | wrk3 remove --all   # compose down -v + worktree remove + state cleanup (never touches main)
wrk3 remove --force feature-a | wrk3 remove --all --force  # same with git worktree remove --force (falls back to rm -rf); for dirty worktrees with modified/untracked files
wrk3 dashboard                       # interactive TUI: worktrees + branches + ports, up/down/add/remove
wrk3 dashboard --project myapp       # same, starting from a registered project
wrk3 update --check                 # show latest release without installing
wrk3 update                         # install latest over the current binary (sha256 verified)
wrk3 proxy status                   # gateway state + per-worktree http://<slug>.localhost URLs
wrk3 proxy up | wrk3 proxy down     # start/stop the gateway (auto-started by up when proxy.enabled)
wrk3 proxy open feature-a           # open the worktree URL in a browser
sudo wrk3 proxy hosts-sync          # 127.0.0.1 entries for Safari/curl (Chrome/FF/Edge need nothing)
wrk3 skill                          # print the bundled agent setup guide (local-setup discovery + compat triage + wrk3.yaml template) to stdout; needs no config
```

Copy includes: `source.git.copy` lists repo-relative files/dirs (globs,
`**` supported) copied from the repo root into each new worktree on `add`
— use it for gitignored files like `.env.local`, `certs/`, or
`storage/*.sqlite`. Missing sources skip with a warning and existing files
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

Main checkout: the repo root is always included implicitly (no state entry)
in `up`/`down` (bare = all including main), `status`/`ls`, and as an
`exec`/`logs` target by branch/slug. It uses reserved port index `-1`
(e.g. `7900` with defaults) with the managed `.env` section ensured on `up`/`down`/`exec`;
`remove` refuses main and `remove --all` covers only managed worktrees.

State recovery: the state file (`.wrk3-state.json` under `worktreeBase`)
is a cache, not the truth. Every read (`status`, `ls`, `up`, `down`,
dashboard) reconciles it against `git worktree list`: on-disk worktrees
missing from state are adopted automatically (ports recovered from the
worktree `.env` when complete and collision-free, else a fresh
allocation; existing `.env` values are never overwritten, divergences
warn). Deleting the state file therefore rebuilds it on next use.
Branches with no checkout never enter state — `add <branch>` (local or
remote ref) and the dashboard create them. Runtime status is synced the
same way: every read probes the configured runner backend (docker compose
today, other orchestrators via the same Runner interface tomorrow) and
persists `running`/`stopped` drift back to the state file (logged as
`synced runtime state: ...`); stacks started out-of-band via plain
`docker compose up` are detected through the worktree folder-name project
as well as the configured `<prefix>-<slug>`. Unknown probes (e.g. daemon
unreachable) never overwrite stored state.

## Dashboard (TUI)

`wrk3 dashboard` opens an interactive view over the current repo plus every
registered project (`tab` switches projects). It polls worktree state and
remote branches (default every 15s, `--poll 0` disables), shows the worktree
table with ports and the next free `app` port, and runs the same operations
as the CLI: `space` selects, `u`/`d` up/down (cursor worktree when nothing
is selected), `a` adds queued branches, `x` removes (asks `y/n`, refuses
main like `remove`), `X` force-removes like `remove --force` (asks
`y/n`, for dirty worktrees with modified/untracked files), `r` refreshes
state, `R` fetches the remote
(`--remote`/`--mine`/`--author`/`--myprs` filter the branch list, `m`
toggles mine, `P` toggles myprs), `1`/`2` or `←`/`→` switch panes, `?`
shows all keys, `q` quits. The unfiltered branch pane unions remote refs
with local-only branches (filtered views stay remote-only); like
`add <branch>`, `a` creates the checkout from either ref, or adopts the
on-disk worktree when one already exists. Worktrees and branches render as tables in
bordered panes (side-by-side on terminals ≥132 cols, stacked otherwise),
the shortcut bar is always visible at the bottom, and the log scrolls.
Pressing `u` flips the selected rows to `setting up` immediately; the
rows keep that status (not `running`) until setup/run entries finish,
even when the setup itself already started containers. Pressing `d`
flips the selected rows to `stopping` immediately until compose down
finishes. Branches with an on-disk worktree missing from state show as
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
# pr-101    pr-101  running  app=8000    demo-pr-101
# pr-102    pr-102  running  app=8100    demo-pr-102

# Run tests inside one worktree without cd'ing there
wrk3 exec pr-101 -- go test ./... -count=1

# Clean up a single stack (volumes included via compose down -v)
wrk3 down pr-102 && wrk3 remove pr-102
```

## Troubleshooting

| Symptom | Likely cause / fix |
| ------- | ------------------ |
| `unknown source type "x" (available sources: [git])` | Typo in `source.type`; only `git` ships in v1. |
| `unknown runner type "x" (available runners: [docker portainer nomad])` | Only `docker` is implemented; stubs return `not implemented`. |
| `project.worktreeBase must not be empty` | `project.worktreeBase` is required. |
| `no wrk3.yaml found ...` | Not inside a repo checkout, or config named differently — `cd` in or pass `-f <path>`. |
| `unknown worktree "foo"` | Name/slug not in state — check `wrk3 status`. |
| `stale` / `?` in status | Worktree directory deleted out-of-band; `remove --force` to clean state, or re-`add`. |
| deleted `.wrk3-state.json` | Self-heals: next `status`/`ls`/`up`/`down`/dashboard run re-adopts on-disk worktrees (ports from `.env` when intact). |
| `already checked out at ... (use add --local ...)` | On-disk worktree missing from state (e.g. state file deleted); `add <branch>` adopts it automatically, or use `add --local`. |
| `pass either branch names or --all, not both` | `remove` takes explicit names **or** `--all`. |
| `--myprs supports GitHub remotes only ...` | The remote URL (`git remote get-url`) is not GitHub — `--myprs` is GitHub-only for now. |
| `github forge needs the gh CLI ...` / `gh is not authenticated ...` | Install `gh` from https://cli.github.com, then run `gh auth login` (wrk3 reuses your session, stores no tokens). |
| Port conflicts | Two checkouts sharing `base`+`step` on one host — give each config a distinct `ports.base` offset or `step`. |
| `sets container_name for service(s) ...` | Compose file pins `container_name:`, which is global and collides across worktrees — delete it (compose generates `<project>-<service>-1`). |
| `Conflict. The container name ... is already in use` | Same cause as above on a stack that predates the preflight check — remove `container_name:` and `docker rm -f` the leftover, then `up` again. |
