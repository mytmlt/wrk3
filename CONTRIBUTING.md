# Contributing to wrk3

Thanks for contributing! This guide covers the workflow, standards, and
checks every PR must pass.

## Code of Conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
Report unacceptable behavior via the channel in [SECURITY.md](SECURITY.md).

## Getting started

Prerequisites: Go ≥ 1.26, `git`, `docker` (for runner E2E only).

```bash
git clone https://github.com/mytmlt/wrk3.git && cd wrk3
go build ./...
go test ./... -count=1
```

Project layout:

- `cmd/` — thin cobra commands; depend on `internal/*` **interfaces only**
  (one `docker` options exception in `cmd/common.go`, see `docs/PLUGINS.md`)
- `internal/project|source|runner|ports|config|forge|proxy` — registry + implementations
- `cmd/skill.md` — bundled `wrk3 skill` guide; `docs/` — user docs

`opencode.json` is an optional editor integration (prompt templates) — safe to ignore.

## Workflow

1. Start from the latest `origin/main`, then create a topic branch +
   worktree for it: `git fetch origin`, then
   `wrk3 fetch` → `wrk3 add feat/short-description --create`
   (equivalent fallback: `git worktree add -b feat/short-description
   <path> origin/main`).
2. Make focused changes. Keep functions small, wrap errors with context
   (`fmt.Errorf("...: %w", err)`), never log secrets.
3. New `Source`/`Runner` backends **must** register in their `registry.go`;
   unknown `source.type`/`runner.type` values error listing available options.
4. State file paths stay absolute (cwd-independence); ports come from the
   state file (`?`/`stale` when the dir is missing).
5. Run the full gate before pushing:
   `go build ./... && go vet ./... && go test ./... -count=1` (or `make test`).
6. Update docs as needed: user-facing changes → `README.md` / `docs/USAGE.md` /
   `docs/CONFIGURATION.md`; notable changes → `CHANGELOG.md` under
   `[Unreleased]`.
7. Open a PR against `main` using the PR template. Link issues and describe
   verification evidence (command logs).

## Commit style

Conventional, imperative, lowercase type:

- `feat: add docker compose runner`
- `fix: handle missing worktree dir in status`
- `docs: clarify port step semantics`
- `chore: bump cobra to v1.10.2`
- `test: cover port collisions`

One logical change per commit; each commit should build and pass tests.

## Versioning and releases

- Semantic Versioning. Maintainers cut releases by tagging:
  `git tag v0.2.0 && git push origin v0.2.0` — GoReleaser builds archives,
  checksums, and GitHub release notes automatically.
- `CHANGELOG.md` follows Keep a Changelog; move `[Unreleased]` entries under
  the new version on release.

## Reporting bugs / requesting features

Use the issue templates (bug report / feature request). Include:

- `wrk3 version` output, OS/arch, Go version if building from source
- Redacted `wrk3.yaml` (strip secrets), exact commands, expected vs actual
- Relevant logs (`wrk3 status`, command output)

Security vulnerabilities: **do not** file public issues — see
[SECURITY.md](SECURITY.md).
