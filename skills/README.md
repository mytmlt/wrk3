# Skills moved

Agent skills now live in the standalone repo
[`mytmlt/wrk3-skills`](https://github.com/mytmlt/wrk3-skills)
(sibling checkout: `../wrk3-skills`):

- `skills/wrk3-compat/SKILL.md` — read-only compatibility check: analyzes a
  project's docker compose files and local setup to decide if it can run
  under wrk3 parallel worktrees. Run first.
- `skills/wrk3-setup/SKILL.md` — author + validate a `wrk3.yaml` (runs the
  compat check as Phase 0).

Copy or symlink the skill dirs into your agent's skill path, e.g.:

```bash
cp -r ../wrk3-skills/skills/wrk3-compat ~/.agents/skills/
cp -r ../wrk3-skills/skills/wrk3-setup ~/.agents/skills/
```
