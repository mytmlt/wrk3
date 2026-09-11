# Usage

Every command accepts the global flags `--project <name>` and
`--config <path>` (one-shot override). Resolution:
`--config` > `--project` > `$WRK3_PROJECT` > current > cwd scan.

## Projects

```bash
wrk3 project add myproject --config ./myproject.yaml
wrk3 project list
wrk3 project use myproject
wrk3 project show [--project myproject]
wrk3 project remove myproject
```

## Daily loop

```bash
wrk3 fetch                          # git fetch --prune, list origin/* refs
wrk3 fetch --mine                   # only branches whose tip commit author matches git config user.name/user.email
wrk3 fetch --author alice           # substring match on author name/email (case-insensitive; repeatable, comma-split)
wrk3 add feature-a feature-b        # worktree add + ports + .env
wrk3 add --select                   # interactive picker over remote branches
wrk3 up feature-a                   # setup entries + compose up + run entry
wrk3 up --all                       # parallel across worktrees (errgroup)
wrk3 status                         # this project
wrk3 status --all                   # every registered project
wrk3 logs feature-a [-f]            # entry.logs command
wrk3 exec feature-a -- <cmd...>     # run inside worktree env (cwd=worktree)
wrk3 down feature-a | wrk3 down --all
wrk3 remove feature-a [--force]     # compose down -v + worktree remove + state cleanup
```

Branch slugs: `feature/foo` → `feature-foo` (max 50 chars). `add`/`up`/`down`
accept branch names or slugs interchangeably; `status` shows
`PROJECT/WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT`, with `?` and
`stale` when a worktree directory is missing.

## Examples

```bash
# Two-review-stack flow
wrk3 add pr-101 pr-102
wrk3 up --all
wrk3 status
# PROJECT    WORKTREE  BRANCH  STATUS   APP   COMPOSE_PROJECT
# myproject  pr-101    pr-101  running  8000  demo-pr-101
# myproject  pr-102    pr-102  running  8100  demo-pr-102

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
| `unknown worktree "foo"` | Name/slug not in state — check `wrk3 status`. |
| `stale` / `?` in status | Worktree directory deleted out-of-band; `remove --force` to clean state, or re-`add`. |
| `pass either branch names or --all, not both` | `up`/`down` take explicit names **or** `--all`. |
| Port conflicts | Two configs sharing `base`+`step` on one host — give each project a distinct `ports.base` offset or `step`. |
