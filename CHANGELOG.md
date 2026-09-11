# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.0.1] - 2026-09-11

### Added

- Initial `wrk3` release: run multiple branches of the same repo in
  parallel as git worktrees, each with isolated ports and a compose project.
- `project add|list|use|remove|show` registry (`~/.config/wrk3/projects.yaml`).
- `fetch`, `add` (incl. `--select`), `up` (parallel setup+run), `down`,
  `logs [-f]`, `status [--all]`, `exec <branch> -- <cmd>`, `remove [--force]`.
- `wrk3.yaml` config with `git` source and `docker` runner,
  deterministic `base + index*step` port allocation, and per-worktree `.env`.
- Stub `portainer`/`nomad` runners returning `not implemented`.
- `fetch --mine` / `--author <name-or-email>`: list only your remote
  branches, filtered by tip-commit author (`--mine` matches git config
  `user.name`/`user.email`; `--author` is a case-insensitive substring
  match, repeatable and comma-split; combined they intersect).
- System-wide installation: `Makefile` (`make install`), `scripts/install.sh`
  (release assets with checksum verification, `go install` fallback), and
  GoReleaser config producing `linux`/`darwin`/`windows` archives.
- Version reporting: `wrk3 version` and `wrk3 --version`, stamped via
  `-ldflags` at build/release time.
- Open-source baseline: `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  `SECURITY.md`, user docs under `docs/`, CI + release workflows, issue/PR
  templates, Dependabot, and the `wrk3-setup` agent skill in `skills/`.
- Plugin authoring guide: `docs/PLUGINS.md` documenting the `Source` and
  `Runner` interfaces, registry, config/`cmd` wiring, and verification.

### Changed

- Generic ports: `ports.base` defaults to `{app: 8000}` with
  `app` required; extra names map to generic `<NAME>_PORT` `.env` vars
  (plus `BASE_URL` family from `app`).
- `wrk3.yaml` no longer takes `project.repo`: the repo root is the
  directory containing the config (the config always lives in the
  project root). `project.worktreeBase` resolves against it.
- `status` shows `PROJECT/WORKTREE/BRANCH/STATUS/APP/COMPOSE_PROJECT`.

### Fixed

- CI pipeline: bump `actions/checkout` v4→v7, `actions/setup-go` v5→v7,
  `golangci/golangci-lint-action` v6→v9 (v6 ships golangci-lint built
  with go1.24 and cannot parse go1.26), and
  `goreleaser/goreleaser-action` v6→v7; fix `errcheck` failures on
  `fmt.Fprint*` to CLI output; make `ports`/`project`/`source` tests
  path-portable and skip linux-only `docker` integration tests on
  Windows so `windows-latest` goes green.

[0.0.1]: https://github.com/mytmlt/wrk3/releases/tag/v0.0.1
