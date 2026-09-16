# Developing plugins (Runner + Source backends)

`wrk3` isolates two extension points behind Go interfaces:

- `Source` (`internal/source/types.go`) — where worktrees come from
  (`git` ships).
- `Runner` (`internal/runner/types.go`) — where worktrees execute
  (`docker` ships; `portainer`/`nomad` are intentional `not implemented`
  stubs — good starting points to copy).
- `Forge` (`internal/forge/forge.go`) — where PR state comes from
  (`github` ships via the `gh` CLI; powers `fetch`/`add`/`dashboard`
  `--myprs`).

There is no dynamic plugin loading: a "plugin" is a new backend
implementation compiled into the binary. `cmd/` must only depend on the
`Source`/`Runner` interfaces — never import a concrete `git`/`docker`
implementation (`cmd/common.go: newSource`/`newRunner` resolve via the
registries). The `Forge` side is thinner: `cmd/myprs.go: myPRBranches`
gates `--myprs` on `forge.Detect` (from `git remote get-url`) and resolves
the provider via the forge registry, so new forges need no `cmd` changes.

## The `Forge` interface (`internal/forge/forge.go`)

```go
type Forge interface {
    Name() string
    MyPRBranches(ctx context.Context, repoPath, remote string) ([]PRBranch, error)
}
```

- `Detect(repoPath, remote)` (`internal/forge/detect.go`) maps the remote
  URL to a kind (`github` for `github.com`; `gitlab`/`gitea` by host
  substring — reserved for future providers; everything else `unknown`).
  `myPRBranches` refuses non-`github` kinds listing `Available()`.
- To add a provider (e.g. GitLab): implement `Forge` in
  `internal/forge/<name>.go` (shell out to `glab` like `github.go` shells
  to `gh`: `exec.LookPath` guard with install/auth guidance, per-call
  timeout, stderr wrapped with `%w`), `Register` it in an `init()`, add
  `<name>_test.go` with pure parse helpers, and document in
  `docs/CONFIGURATION.md` (`--myprs` section) + `CHANGELOG.md`.

## Interfaces

### `Source` (`internal/source/types.go`)

```go
type Source interface {
    Fetch(repoPath, remote string) error
    Refs(repoPath, remote string) ([]string, error)
    LocalBranches(repoPath string) ([]string, error)
    RefsDetailed(repoPath, remote string) ([]BranchRef, error)
    DefaultBranch(repoPath, remote string) (string, error)
    BranchHistory(repoPath, remote, branch, base string, limit int) ([]BranchRef, error)
    Identity(repoPath string) (name, email string, err error)
    Add(repoPath, branch, worktreePath, remote string) error
    AddNew(repoPath, branch, worktreePath, base string) error
    Remove(repoPath, worktreePath string, force bool) error
    Pull(worktreePath string, opts PullOptions) error
    List(repoPath string) ([]WorktreeInfo, error)
}
```

Semantics (match `internal/source/git.go`):

- `Fetch(repoPath, remote)` — refresh remote refs (git: `fetch <remote> --prune`; empty remote means `origin`).
- `Refs(repoPath, remote)` — list known remote branches (git: `branch -r`,
  `<remote>/*`, HEAD symref skipped, other remotes filtered out).
- `LocalBranches(repoPath)` — list local branches (git: `branch`,
  `refs/heads/*`). Powers worktree creation from local-only branches
  (dashboard branch pane and `add` picker union).
- `RefsDetailed(repoPath, remote)` — same branches as `[]BranchRef`
  (`Name` short branch, `AuthorName`/`AuthorEmail`/`CommitterName`/
  `CommitterEmail`/`CommitterDate` from the tip commit;
  git: `for-each-ref` over `refs/remotes/<remote>`). Powers
  `fetch --mine` / `--author` and `add --remote --mine` (tip fast path),
  plus the dashboard branch-pane ordering (priority + recency).
- `DefaultBranch(repoPath, remote)` — short default-branch name (git:
  `symbolic-ref refs/remotes/<remote>/HEAD`, else `main`/`master` probe;
  empty when unknown). Powers the `--mine`/`--author` history fallback.
- `BranchHistory(repoPath, remote, branch, base string, limit int)` —
  up to `limit` branch-exclusive commits (`git log <remote>/<branch>
  --not <remote>/<base>`, newest first; empty base = plain tip log;
  branch == base = empty). Powers the `--mine`/`--author` history
  fallback (bot/cursor tips you pushed).
- `Identity(repoPath)` — local git identity (`git config user.name` /
  `user.email`; empty when unset). Powers `fetch --mine`.
- `Add(repoPath, branch, worktreePath, remote)` — provision one worktree dir.
  Error on empty `branch`/`worktreePath` (`fmt.Errorf("...: %w", err)`).
  When the branch has no local ref but `<remote>/<branch>` exists, create
  a tracking branch (`worktree add --track -b`).
- `AddNew(repoPath, branch, worktreePath, base)` — provision one worktree
  dir with a new local branch starting at `base`
  (`worktree add -b <branch> <path> <base>`).
- `Remove(repoPath, worktreePath string, force bool)` — delete it.
- `Pull(worktreePath string, opts PullOptions)` — pull the worktree's
  branch from its upstream (git: `-C <worktreePath> pull`
  with `--rebase`/`--ff-only` from `opts`; the two flags are mutually
  exclusive). Powers `wrk3 pull`.
- `List(repoPath)` — return existing worktrees as `[]WorktreeInfo`
  (`Path` absolute, `Branch` short name, `Commit` SHA, `Bare` flag).
  Keep paths absolute — state file paths stay absolute
  (cwd-independence).

Slug note: branch → directory mapping (`feature/foo` → `feature-foo`,
max 50 chars) lives in `internal/source/slug.go` and is backend-agnostic.
Your backend receives the already-joined absolute `worktreePath`.

### `Runner` (`internal/runner/types.go`)

```go
type Runner interface {
    Up(ctx context.Context, worktreePath string, env map[string]string) error
    Down(ctx context.Context, worktreePath string, env map[string]string) error
    Logs(ctx context.Context, worktreePath string, follow bool) (string, error)
    Exec(ctx context.Context, worktreePath string, cmd []string, env map[string]string) error
    Status(ctx context.Context, worktreePath string) (Status, error)
}
```

Semantics (match `internal/runner/docker.go`):

- `Up` — start the worktree (docker: `compose up -d --build`).
  `Down` removes containers/networks but preserves volumes; volume
  reclamation belongs to the `remove` path (`down -v` there).
- `Exec` — run `cmd` as a host process with `cwd=worktreePath` and
  `env` applied. Used for `entry.setup`/`run`/`stop` strings, which
  `cmd/common.go: shellCmd` wraps as `sh -c "<entry string>"` — execute
  them verbatim, never hardcode repo-specific commands (e.g. `make test`)
  in Go code.
- `Logs` — return logs; with `follow=true` block until `ctx` cancels.
- `Status` — return `Status{State: StateRunning|StateStopped|StateUnknown}`.
  `StateUnknown` + error when the backend is unreachable.
- `env` is the allocated-ports map (`allocated = base + index*step`,
  see `docs/CONFIGURATION.md`). If your backend needs a forced variable
  (docker forces `COMPOSE_PROJECT_NAME`), set it in the backend, not in
  `cmd` or `.env`.
- Bound every external invocation with a timeout (`context.WithTimeout`;
  docker uses 5 min, git 60 s) and wrap errors with context:
  `fmt.Errorf("myrunner up (dir=%s): %w", worktreePath, err)`.

`runner.Options{ComposeFiles, ProjectPrefix, Slug}` is docker-specific.
Generic backends should take their own options struct (or none, like the
`PortainerRunner{}`/`NomadRunner{}` stubs).

## Step-by-step: new Runner

1. **Implement the interface** in `internal/runner/<name>.go`:

   ```go
   package runner

   type MyRunner struct{ opts MyOptions }
   var _ Runner = (*MyRunner)(nil)

   func NewMyRunner(opts MyOptions) *MyRunner { return &MyRunner{opts: opts} }

   func init() {
       Register("myrunner", func() Runner { return NewMyRunner(MyOptions{}) })
   }
   // ... Up/Down/Logs/Exec/Status ...
   ```

   Start by copying `portainer.go` (stub) or `docker.go` (full `os/exec`
   example: `composeArgs`/`buildEnv` pure helpers, per-call timeout,
   stderr captured into the returned error).

2. **Register in `internal/runner/registry.go`** via the `init()` above.
    This is mandatory — unknown `runner.type` values error listing
    `Available()` (see `USAGE.md` troubleshooting).

3. **Wire config** in `internal/config/types.go`:
   - Add `MyConfig` struct + field on `RunnerConfig` (e.g. `My MyConfig`).
   - Extend `Validate()`: after the `runner.Resolve` check, validate your
     section when `Runner.Type == "myrunner"` (required fields non-empty),
     mirroring the `docker` branch (`composeFiles` non-empty,
     `projectPrefix` non-empty).
   - Document fields in `docs/CONFIGURATION.md` and `wrk3.yaml.example`
     if the backend should be user-visible.

4. **Wire construction** in `cmd/common.go: newRunner`. Every backend is
    built via its registered `Factory(Options)` with
    `cfg.ComposeOptions(slug)` (compose files + prefix + slug). If your
    backend needs per-worktree options, read them from `Options` — still
    via the `runner` package API, never by importing your concrete type's
    internals beyond its constructor.

5. **Test + docs**: unit test next to the code (`<name>_test.go`, pure
   helpers + hermetic temp dirs; never touch the user's registry or
   checkouts), then update `README.md` (features list), `docs/USAGE.md`
   (error table), and `CHANGELOG.md` `[Unreleased]`.

## Step-by-step: new Source

1. **Implement the interface** in `internal/source/<name>.go`:

   ```go
   package source

   type MySource struct{ Timeout time.Duration }
   var _ Source = (*MySource)(nil)

   func NewMySource() Source { return &MySource{} }

   func init() {
       Register("mysource", NewMySource)
   }
   // ... Fetch/Refs/Add/Remove/List ...
   ```

   Model on `internal/source/git.go`: per-call timeout
   (`defaultGitTimeout = 60s`), `os/exec` + captured stderr wrapped with
   `fmt.Errorf("...: %w", err)`, pure parse helpers (`parseRefs`,
   `parseWorktreePorcelain`) covered by unit tests.

2. **Register in `internal/source/registry.go`** via the `init()` above.
   Mandatory — `config.Validate` resolves `source.type` and errors
   listing available sources otherwise.

3. **Wire config** in `internal/config/types.go`:
   - Add `MySourceConfig` + field on `SourceConfig`.
   - Extend `Validate()` for your type when selected (same pattern as
     the runner side).
   - Document in `docs/CONFIGURATION.md`.

4. **Construction needs no `cmd` change**: `cmd/common.go: newSource`
    already builds any registered source via `source.Resolve(... )()`.
    Only touch `cmd` if your backend needs per-call options beyond
    `(repoPath, remote, branch, worktreePath)` — prefer keeping the interface
    signature and reading options from config inside the backend.

5. **Test + docs**: same as runners — `<name>_test.go` with temp git
   repos/dirs (see `git_test.go`), plus `README.md` / `docs/USAGE.md` /
   `CHANGELOG.md` updates.

## Rules every backend must follow

- Keep functions small, wrap errors with `%w`, no secrets in logs/commits.
- Never hardcode repo-specific commands; `entry.setup`/`run`/`stop`/`logs`
  strings execute verbatim via `sh -c` with `cwd=worktree`, `env=ports`.
- State paths stay absolute; `status` shows `?`/`stale` for missing dirs
  instead of failing hard.
- Don't commit personal configs (`wrk3.*.local.yaml`), `.wrk3-state.json`,
  `.worktrees/`, `.env` files, or binaries.

## Verification

```bash
go build ./... && go vet ./... && go test ./... -count=1   # full gate (make test)
```

`internal/runner` tests take ~10s (real `docker`); everything else is
fast/hermetic. For CLI-level checks use a
throwaway repo — never the real one:

```bash
go run . -f ./wrk3.yaml.example status
```

`add`/`up` create real worktrees/containers — only run them against
throwaway repos, and `down` + `remove` afterwards.
