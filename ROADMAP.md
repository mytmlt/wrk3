# Roadmap

Project goal: **turn any codebase into a `wrk3.yaml` that runs the app
the way its developers run it locally** — as parallel git worktrees with
isolated ports and per-worktree runners — whether that means
`docker`, `podman`, `portainer`, `nomad`, or a bare machine.

The setup path is: learn the manual onboarding first (docs, scripts,
env files, install → migrate/seed → run), ask the developer when unsure,
then encode it as `wrk3.yaml`.
Every `entry.*` command must trace back to onboarding evidence or a
developer answer — never invented.

## Where we are

- [x] `git` source: `fetch` / `add` / `worktree list` over parallel worktrees.
- [x] `docker` runner: `compose -p <prefix>-<slug>` per worktree, isolated ports + `.env`.
- [x] `podman` runner: same shape via native `podman compose`.
- [x] `source.git.copy`: seed gitignored local-state files into new worktrees.

## Next

- [x] Bare-machine / local runner: run `entry.setup` / `entry.run` /
  `entry.stop` directly on the host (no compose) for apps with no
  container stack. Ports, `.env`, and compose project are omitted unless
  `ports` is set. `status` uses the stored up/down lifecycle
  (`running` = last up succeeded).
- [ ] `portainer` runner: implement the stub behind the `Runner`
  interface (currently `not implemented`).
- [ ] `nomad` runner: implement the stub behind the `Runner` interface.
- [ ] Onboarding coverage: compose + bare-machine onboarding for the common
  stacks (Node, Python, Go, Rust, Ruby) so a first-pass config works,
  asking the developer only for secrets and genuine ambiguities.

## Non-goals

- Replacing onboarding docs — wrk3 encodes them, it does not substitute
  for them.
- Guessing setup commands — uncertain agents ask, they do not invent.
- Sharing mutable host state between worktrees — isolation is the point.
