# Skills

The bundled source of truth is `wrk3 skill` (`cmd/skill.md` in this
repo): onboarding-first discovery (learn the manual setup, ask the
developer when unsure) → compat triage → author + validate `wrk3.yaml`.
The project goal and runner coverage live in [`ROADMAP.md`](../ROADMAP.md).

Extended skills also live in the standalone repo
[`mytmlt/wrk3-skills`](https://github.com/mytmlt/wrk3-skills)
(clone it next to this repo or anywhere on your machine):

- `skills/wrk3-compat/SKILL.md` — read-only compatibility check: analyzes a
  project's docker compose files and local setup to decide if it can run
  under wrk3 parallel worktrees. Run first.
- `skills/wrk3-setup/SKILL.md` — author + validate a `wrk3.yaml` (runs the
  compat check as Phase 0).

Copy or symlink the skill dirs into your agent's skill path, e.g.:

```bash
git clone https://github.com/mytmlt/wrk3-skills.git
cp -r wrk3-skills/skills/wrk3-compat ~/.agents/skills/
cp -r wrk3-skills/skills/wrk3-setup ~/.agents/skills/
```
