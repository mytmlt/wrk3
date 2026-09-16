# Roadmap

Project goal: **turn any codebase into a `wrk3.yaml` that runs the app
the way its developers run it locally** — as parallel git worktrees with
isolated ports and per-worktree runners — whether that means
`docker`, `podman`, `portainer`, `nomad`, or a bare machine.

The agent path is: learn the manual onboarding first (docs, scripts,
env files, install → migrate/seed → run), ask the developer when unsure,
then encode it as `wrk3.yaml` (`run wrk3 skill` for the full guide).
Every `entry.*` command must trace back to onboarding evidence or a
developer answer — never invented.

## Where we are

- [x] `git` source: `fetch` / `add` / `worktree list` over parallel worktrees.
- [x] `docker` runner: `compose -p <prefix>-<slug>` per worktree, isolated ports + `.env`.
- [x] `podman` runner: same shape via native `podman compose`.
- [x] `source.git.copy`: seed gitignored local-state files into new worktrees.
- [x] Bundled agent skill (`wrk3 skill`): onboarding-first setup guide.

## Next

- [ ] Bare-machine / local runner: run `entry.setup` / `entry.run` /
  `entry.stop` directly on the host (no compose) for apps with no
  container stack. The skill already captures these flows; the runner
  is what is missing.
- [ ] `portainer` runner: implement the stub behind the `Runner`
  interface (currently `not implemented`).
- [ ] `nomad` runner: implement the stub behind the `Runner` interface.
- [ ] Skill coverage: compose + bare-machine onboarding for the common
  stacks (Node, Python, Go, Rust, Ruby) so `wrk3 skill` produces a
  working config on the first pass, asking the developer only for
  secrets and genuine ambiguities.

## Non-goals

- Replacing onboarding docs — wrk3 encodes them, it does not substitute
  for them.
- Guessing setup commands — uncertain agents ask, they do not invent.
- Sharing mutable host state between worktrees — isolation is the point.
