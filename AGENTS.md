# AGENTS.md — wrk3

`wrk3` runs multiple branches of the same repo in parallel as git worktrees,
each isolated with its own ports and container project. Single static Go
binary (`Go ≥ 1.26`); runtime deps are `git` and a container engine
(`docker` for the `docker` runner, `podman` for the `podman` runner).

## Agent development workflow (binding)

Every code or docs task runs on an isolated branch + worktree, with
tests. The agent commits, pushes, opens PRs, and watches checks
autonomously without asking (standing pre-authorization); the human
merges when green. See the full
checklist in `skills/wrk3-dev-flow/SKILL.md`.

> **Factory override (SuperPlane/canvas — wins).** If `$SUPERPLANE_TASK_DIR` /
> `$SUPERPLANE_RESULT_FILE` / `$TASK_BROKER_*` is set, cwd ends in `/repo` with a
> `.superplane` parent, or the prompt says "Do not create or open the pull request":
> skip `wrk3 fetch/add/up`, work in the current checkout (`git checkout -b
> <type/short-slug> origin/$BASE`, push `git push -u origin HEAD`), never create
> worktrees/PRs or watch checks — canvas opens the PR. See `skills/wrk3-dev-flow/SKILL.md` "Factory override".

1. **Isolate.** Always start new work from the latest `origin/main`:
   `git fetch origin`, then `wrk3 fetch` → `wrk3 add <branch> --create`
   (creates `type/short-slug` + worktree from `origin/<default>`;
   `feat/`, `fix/`, `docs:`, `chore:`, `test:` per `CONTRIBUTING.md`).
   `git worktree add` fallback only when no `wrk3.yaml` resolves — log
   why — and it must also start from latest main:
   `git worktree add -b <branch> <path> origin/main`.
   Never implement in the user's checkout or on `main`. Run `wrk3 up`
   only when the repo has a runnable `docker` stack; otherwise work
   directly in the worktree (Go gate below).
2. **Test.** New functionality → new co-located `*_test.go` covering it.
   Behavior change → update the existing tests for those paths.
   Finish with the full gate green (Essential commands above), then always
   run `ocr review --from origin/main --to $(git branch --show-current)`
   and clear every finding (fix → gate → re-review) before committing.
3. **Autonomy (standing pre-authorization — never ask).** Committing,
   pushing the feature branch, opening the PR, and watching checks are
   pre-authorized once and for all: never ask the human whether to
   commit, push, open the PR, or keep waiting on checks — just do it
   and report the PR URL plus check results. PR title/body
   follows `.github/PULL_REQUEST_TEMPLATE.md` with Verification
   evidence. Standing bash permissions for this flow live in project
   `opencode.json` (`permission.bash`); pushes to `main`/`master`, tag
   pushes, and any merge are denied there. Never push to `main`, never `--force-push` (rebase +
   re-run the gate instead), never merge anything — merging is the
   human's job.
4. **After PR open.** Watch checks with `gh pr checks <number> --watch`
   until they finish; fix failures with new commits on the same branch
   (commit, push, re-watch). When everything is green, the agent is
   done — the human merges. The agent never merges: no `gh pr merge`,
   no GitHub UI merges, no local merges, never on red.

## Essential commands

```bash
go build ./... && go vet ./... && go test ./... -count=1   # full gate
make test        # same via Makefile
make build       # version-stamped ./bin/wrk3
./bin/wrk3 --help && ./bin/wrk3 version
```

Run the full gate before finishing any code change. `internal/runner` tests
take ~10s (they exercise real `docker`); everything else is fast and
hermetic (temp git repos, temp dirs — no network).

## Layout and architecture (binding)

- `main.go` → `cmd/` (thin cobra commands) → `internal/*` **interfaces only**
  (see `docs/PLUGINS.md` for the one compose-options exception in `cmd/common.go`).
- `internal/config/` — `wrk3.yaml`/`wrk3.yml` load + validation + upward
  discovery (`discover.go`; `docs/CONFIGURATION.md` is the field reference).
- `internal/source/` — `Source` iface + `registry.go` + `git.go`.
- `internal/runner/` — `Runner` iface + `registry.go` + `docker.go` + `podman.go`
  (`portainer`/`nomad` are intentional `not implemented` stubs).
- `internal/ports/` — `allocated = base + index*step` allocator, `.env`
  writer, `<worktreeBase>/.wrk3-state.json` state file.
- `internal/forge/` — `gh`-based PR filtering (`--myprs`).
- `internal/proxy/` — stdlib local gateway (`<slug>.<domain>` → app port).
- `internal/project/` — `~/.config/wrk3/projects.yaml` registry.
- `cmd/dashboard*.go` — bubbletea TUI; `cmd/skill.md` — bundled `wrk3 skill` guide.

## Rules

- New `Source`/`Runner` backends **must** register in their `registry.go`;
  unknown `source.type`/`runner.type` values error listing available options.
- State file paths stay absolute (cwd-independence). `status` shows
  `?`/`stale` when a worktree dir is missing — never fail hard there.
- Branch slugs: `feature/foo` → `feature-foo`, max 50 chars
  (`internal/source/slug.go`).
- Entry strings (`setup`/`run`/`stop`/`logs`) execute verbatim via `sh -c`
  with `cwd=worktree`, `env=allocated ports` — never hardcode repo-specific
  commands (e.g. `make test`) in Go code.
- Keep functions small, wrap errors with context
  (`fmt.Errorf("...: %w", err)`), no secrets in logs or commits.

## Config and resolution

- Example: `wrk3.yaml.example`. Resolution order for every command:
  `-f/--file <path>` > upward scan from cwd for `wrk3.yaml`, then `wrk3.yml`
  (nearest directory wins). No registry, no env var — like `docker compose`.
- For config-authoring questions (new stack, broken config, port mapping),
  run `wrk3 skill` first (bundled guide in `cmd/skill.md`). Extended skills
  live in the standalone `wrk3-skills` repo
  (`https://github.com/mytmlt/wrk3-skills` — `skills/wrk3-compat/SKILL.md`
  for the read-only compose/local-setup compatibility triage, then
  `skills/wrk3-setup/SKILL.md` to author the config).
  See `skills/README.md`.

## Verifying behavior changes

Unit tests live next to the code (`*_test.go`). For CLI-level verification,
use a temp git repo — never touch the user's real checkouts:

```bash
go run . -f ./wrk3.yaml.example status
```

`add`/`up` create real worktrees/containers — only run them against throwaway
repos, and `down` + `remove` afterwards to clean up.

## Don'ts

- Don't commit personal configs (`wrk3.*.local.yaml`), `.wrk3-state.json`, `.worktrees/`, `.env` files,
  binaries, or anything with absolute personal paths / secrets.
- User-facing change? Update `README.md` / `docs/USAGE.md` /
  `docs/CONFIGURATION.md` plus a `CHANGELOG.md` `[Unreleased]` entry.
- Security issues: see `SECURITY.md` — never file public issues for them.
