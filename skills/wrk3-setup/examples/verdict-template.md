# Compat verdict template (copy into chat — never commit filled copies)

## wrk3 compat verdict: <COMPATIBLE | COMPATIBLE WITH CHANGES | INCOMPATIBLE>

**Repo root:** <path> | **Compose files:** <list> | **Services:** <names>

| Check | Result | Evidence |
| ----- | ------ | -------- |
| C1 container_name | pass/fail | <file:line> |
| C2 host ports parameterized | pass/fail | <file:line> |
| C3 host network mode | pass/fail | <file:line> |
| C4 bind mounts | pass/fail | <file:line> |
| C5 setup reproducible | pass/fail | <file:line> |
| C6 base+ranges | pass/fail | <bases + ranges; `ports.step` must be absent> |
| C7 urls/proxy/shared/health | pass/fail | <block or n/a> |

**Required changes (if any):**
1. <file:line — exact edit>
2. <...>

**Proposed ports.base mapping:**

| wrk3 name | .env var | compose host binding | default |
| --------- | -------- | -------------------- | ------- |
| app | APP_PORT | <service ports entry> | <n> |

**Suggested entry commands (repo verbs only):**
setup: [...] | run: "..." | stop: "..." | logs: "..." | reload: [...]
