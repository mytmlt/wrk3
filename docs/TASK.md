# Task definition

`wrk3` analyzes a project's docker compose files (or a host plan) into an
**internal task definition**, then projects that definition onto other
environments. Source files are never modified.

```bash
wrk3 task                     # discover compose in cwd → internal YAML
wrk3 task --to compose        # isolation-safe compose (parameterized ports)
wrk3 task --to swarm          # docker stack / swarm
wrk3 task --to portainer      # Portainer stack (compose, no container_name)
wrk3 task --to host           # docker run / process plan
wrk3 task --from host plan.yaml --to compose
```

Needs no `wrk3.yaml`. Notes (warnings / blocks) go to stderr; the document
goes to stdout.

## Why

The same stack should run as:

- local `docker compose` (including parallel wrk3 worktrees)
- a Portainer stack
- Docker Swarm (`docker stack deploy`)
- processes / `docker run` on the machine

without rewriting the project's compose files. The Task is the portable
middle form.

## Internal YAML

```yaml
name: demo
source:
  kind: compose          # compose | host
  files: [docker-compose.yml]
services:
  - name: web
    image: nginx:alpine  # and/or build.context
    command: []          # exec form
    commandShell: ""     # shell form
    env:
      FOO: bar
    ports:
      - name: app
        host: 8000
        container: 8000
        protocol: tcp
        hostVar: APP_PORT   # renderers emit ${APP_PORT:-8000}:8000
    mounts:
      - type: bind         # bind | volume | tmpfs
        source: .
        target: /app
    dependsOn: [db]
  - name: db
    image: postgres:16-alpine
volumes:
  - name: pgdata
```

`hostVar` is how wrk3 remaps host ports per worktree without touching
source. Hardcoded `"8000:8000"` is recorded and re-emitted as
`${APP_PORT:-8000}:8000` on projection. `container_name` is recorded on
the service (`fixedContainerName`) and **stripped** on every projection
(it is global on the docker daemon).

## Environments

| `--to` | Result |
| ------ | ------ |
| `internal` | Canonical Task YAML (default). |
| `compose` | Compose file: parameterized ports, no `container_name`. Host-only processes get `build: .` so they can round-trip. |
| `portainer` | Same compose shape, safe to paste as a Portainer **Compose** stack. Use `--to swarm` when the Portainer endpoint is Swarm. |
| `swarm` | Overlay networks, `deploy.replicas`, long-form ingress ports. Bind mounts are dropped. Services without an `image` **block** (`docker stack deploy` does not build). |
| `host` | Per-service `exec`: `docker run` (and `docker build` when needed) or a host process command. Re-analyzable with `--from host`. |

## Host plan (`--from host`)

```yaml
name: local
services:
  app:
    command: python -m http.server 8000
    workdir: .
    port: 8000
  redis:
    image: redis:7
    port: 6379
```

`--to compose` turns this into a compose file (process services get a
build context; image services stay images). `--to host` from compose
does the reverse via `docker run`.

## Notes

| Code | Meaning |
| ---- | ------- |
| `container_name` | Recorded, stripped on project. |
| `hardcoded_port` | Will be parameterized via `hostVar`. |
| `host_network` | Warn; **block** on swarm. |
| `bind_mount` | Warn on swarm (dropped). |
| `missing_image` | **Block** on swarm. |
| `privileged` | Warn on swarm/portainer. |
