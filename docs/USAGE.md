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
wrk3 fetch --mine                   # only branches whose tip commit author or committer matches git config user.name/user.email
wrk3 fetch --author alice           # substring match on author/committer name/email (case-insensitive; repeatable, comma-split)
wrk3 add feature-a feature-b        # worktree add + ports + .env
wrk3 add                            # bare = interactive picker over remote branches
wrk3 add --remote upstream --mine   # fetch upstream, create all my branches (skip registered/checked-out)
wrk3 add --remote upstream          # fetch upstream, create all its branches
wrk3 add --remote upstream hotfix   # create one branch, tracking upstream when remote-only
wrk3 add --local                    # adopt all existing local worktrees (no fetch from origin)
wrk3 add --local feature-a          # adopt only the named local worktree(s)
wrk3 up feature-a                   # setup entries + compose up + run entry
wrk3 up                             # bare = all worktrees, in parallel (errgroup)
wrk3 status                         # this config (full table)
wrk3 ls                             # this config (minimal WORKTREE/BRANCH/STATUS/APP)
wrk3 ls --project myapp             # worktrees in a registered project, from anywhere
wrk3 project ls                     # all auto-registered projects (NAME/CONFIG/WORKTREES)
wrk3 logs feature-a [-f]            # entry.logs command
wrk3 exec feature-a -- <cmd...>     # run inside worktree env (cwd=worktree)
wrk3 down feature-a | wrk3 down     # bare = all
wrk3 remove feature-a feature-b | wrk3 remove --all   # compose down -v + worktree remove + state cleanup
wrk3 update --check                 # show latest release without installing
wrk3 update                         # install latest over the current binary (sha256 verified)
```

Branch slugs: `feature/foo` → `feature-foo` (max 50 chars). `add`/`up`/`down`
accept branch names or slugs interchangeably; `status` shows
`WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT`, with `?` and
`stale` when a worktree directory is missing.

## Examples

```bash
# Two-review-stack flow
wrk3 add pr-101 pr-102
wrk3 up
wrk3 status
# WORKTREE  BRANCH  STATUS   APP   COMPOSE_PROJECT
# pr-101    pr-101  running  8000  demo-pr-101
# pr-102    pr-102  running  8100  demo-pr-102

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
| `pass either branch names or --all, not both` | `remove` takes explicit names **or** `--all`. |
| Port conflicts | Two checkouts sharing `base`+`step` on one host — give each config a distinct `ports.base` offset or `step`. |
| `sets container_name for service(s) ...` | Compose file pins `container_name:`, which is global and collides across worktrees — delete it (compose generates `<project>-<service>-1`). |
| `Conflict. The container name ... is already in use` | Same cause as above on a stack that predates the preflight check — remove `container_name:` and `docker rm -f` the leftover, then `up` again. |
