# Skills

Agent skills vendored in this repo (canonical source — see `AGENTS.md`).
Supersedes the former standalone `mytmlt/wrk3-skills` repo, which is kept
for history only; update skills here together with `docs/` and
`CHANGELOG.md` on every behavior change.

| Skill | Path | Use when |
| ----- | ---- | -------- |
| `wrk3-setup` | `skills/wrk3-setup/SKILL.md` | Check compatibility, propose fixes if incompatible, then author + validate a `wrk3.yaml`. |

Each skill carries `wrk3-version` frontmatter naming the CLI release it was
tested against. Keep this table in sync when testing against a new release.

| skills release | tested wrk3 |
| -------------- | ----------- |
| 0.2.0 | `wrk3 v0.10.25` (covers `main`: range ports, aliases, `urls`, `podman`, `health`, `proxy`) |
| 0.1.0 | `wrk3 v0.1.0` (initial extraction — outdated, kept for history) |

Usage with agents: copy or symlink the skill dir your agent loads, e.g.
`cp -r skills/wrk3-setup ~/.agents/skills/`, or point your agent at
`skills/wrk3-setup/SKILL.md` directly.
