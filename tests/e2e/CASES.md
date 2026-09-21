# E2E cases (issue #16)

Mirror of the scripted CLI scenarios in `tests/e2e/` (build tag `e2e`).
Run with `go test -tags e2e ./tests/e2e/ -count=1 -v`.
Each case uses a throwaway repo (`origin.git` bare + `repo` checkout,
`main` + `feature/foo` pushed, isolated `HOME`/`XDG_CONFIG_HOME`) and
never touches real checkouts.

## H — Happy path (PR1)

| ID | Scenario | Go test |
|----|----------|---------|
| H01 | `fetch` lists the pushed `feature/foo` branch | `TestE2E_FetchListsBranch` |
| H02 | `add feature/foo --no-create` creates `.worktrees/feature-foo`, state holds the absolute path + `app` 8001, worktree `.env` holds `APP_PORT=8001` | `TestE2E_AddExistingBranch` |
| H03 | `status` after add shows `feature/foo` with `app=8001` | `TestE2E_StatusShowsWorktree` |
| H04 | `up feature/foo` → `logs feature/foo` → `down feature/foo` each exit 0 (echo entries + minimal alpine compose stack) | `TestE2E_UpLogsDown` |
| H05 | `remove feature/foo --force` deletes the dir; `status` still exits 0 | `TestE2E_RemoveCleansUp` |
| H06 | `up feature/foo` after remove errors with `unknown worktree` | `TestE2E_UpAfterRemoveErrors` |

## E — Edge cases (PR2)

| ID | Scenario | Go test |
|----|----------|---------|
| E01 | `add feat/new --create` creates branch from origin default, state holds it | `TestE2E_CreateWithFlag` |
| E02 | `add feat/unknown --no-create` fails fast with `--create` hint, creates nothing | `TestE2E_NoCreateFailsFast` |
| E03 | bare `add feat/new2` with EOF stdin cancels with `cancelled` + `--create` hint | `TestE2E_CreatePromptEOF` |
| E04 | two adds allocate `app=8001` then `app=8002` (base 8000, upward by 1) | `TestE2E_PortAllocation` |
| E05 | `SECRET=topsecret` appended to worktree `.env` survives `up` with `APP_PORT` gap-filled | `TestE2E_EnvPreservesSecrets` |
| E06 | leaked `APP_PORT=5000` is overwritten to the allocated port on `up` | `TestE2E_EnvOverwritesManagedPorts` |
| E07 | deleting `.wrk3-state.json` then `status` re-adopts the on-disk worktree | `TestE2E_AdoptsOrphanAfterStateLoss` |
| E09 | fresh `status` lists implicit `main` at `app=8000`; `remove main` refuses | `TestE2E_MainCheckout` |
| E10 | `exec feature/foo -- sh -c 'test "$APP_PORT" = 8001'` exits 0 | `TestE2E_ExecEnv` |
| E11 | deleted worktree dir shows `stale`/`?` with exit 0; `remove --force` drops it | `TestE2E_StaleDir` |

## D — Docker (PR3)

Opt-in real-daemon scenarios gated on `WRK3_E2E_DOCKER=1` (skipped in
`-short`, skipped when `docker info` fails). Compose stack is
`alpine:3.19` + `sleep infinity`.

| ID | Scenario | Go test |
|----|----------|---------|
| D01 | `add` → `up` → `status` (exit 0) → `down` on the alpine stack | `TestE2E_DockerUpDown` |
| D02 | two worktrees `up` together, both visible in `docker ps`, `down` both | `TestE2E_DockerParallel` |
