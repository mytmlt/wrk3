---
name: wrk3-dev-flow
description: Use ONLY when implementing a code or docs task in the wrk3 repo itself. Covers the binding agent workflow: isolate on branch + worktree, full Go gate, autonomous commit/push/PR/checks-watching with gh pr checks --watch. Triggers on wrk3 repo tasks, feature branches, PR creation, and CI check follow-ups.
---

# wrk3-dev-flow

Binding workflow for every code or docs task in this repo. The human
merges when green; everything before that is the agent's job.

## Standing autonomy (never ask)

Committing, pushing the feature branch, opening the PR, and watching
checks are pre-authorized once and for all. Never ask the human whether
to commit, push, open the PR, or keep waiting on checks — just do it
and report the PR URL plus check results. Running builds, vets, tests,
and linters never needs approval either.

Standing bash permissions for this flow live in project `opencode.json`
(`permission.bash`: `wrk3 *`, `./bin/wrk3 *`, `make *`,
`golangci-lint *`, `git rebase *`, `gh pr create|edit *`,
`git push *origin*` on topic branches). Denied there on purpose:
pushes to `main`/`master`, tag pushes, `git merge *`, `gh pr merge *`.
If a command is held for approval anyway (stale config), say so and
continue with what is allowed.

## Phase 1 — isolate

1. Always start new work from the latest `origin/main`: `git fetch
   origin`, then `wrk3 fetch` → `wrk3 add <branch> --create` (creates
   `type/short-slug` + worktree from `origin/<default>`, i.e. latest
   main; `feat/`, `fix/`, `docs:`, `chore:`, `test:` per
   `CONTRIBUTING.md`). `--create` skips the prompt so scripts never
   hang; never `git checkout -b` in the main checkout.
2. `git worktree add` fallback only when no `wrk3.yaml` resolves — log
   why — and it must also start from latest main:
   `git worktree add -b <branch> <path> origin/main`.
3. Never implement in the user's checkout or on `main`. Run `wrk3 up`
   only when the repo has a runnable `docker` stack; otherwise work
   directly in the worktree.

## Phase 2 — implement + test

- New functionality → new co-located `*_test.go` covering it. Behavior
  change → update the existing tests for those paths.
- Keep functions small, wrap errors with context
  (`fmt.Errorf("...: %w", err)`), no secrets in logs or commits.
- User-facing change → update `README.md` / `docs/USAGE.md` /
  `docs/CONFIGURATION.md` plus a `CHANGELOG.md` `[Unreleased]` entry.
- Finish with the full gate green before every commit:
  `go build ./... && go vet ./... && go test ./... -count=1`
  (`make test` is the same; `internal/runner` tests take ~10s on real
  `docker`, everything else is hermetic).

## Phase 3 — commit + push + PR (autonomous)

- One logical change per commit; each commit builds green. Conventional,
  imperative, lowercase type (`feat:`, `fix:`, `docs:`, `chore:`,
  `test:`).
- Push the feature branch (`git push -u origin <branch>`). Never push to
  `main`, never `--force-push` (rebase + re-run the gate instead),
  never push tags (maintainers cut releases).
- Open the PR with `gh pr create` against `main`, following
  `.github/PULL_REQUEST_TEMPLATE.md` with Verification evidence
  (gate logs, `status` output).

## Phase 4 — watch checks (autonomous)

- Watch with `gh pr checks <number> --watch` until everything finishes;
  fix failures with new commits on the same branch (commit, push,
  re-watch).
- When everything is green, the agent is done — report the PR URL plus
  check results and stop. The agent never merges: no `gh pr merge`, no
  GitHub UI merges, no local merges, never on red.
