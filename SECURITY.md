# Security Policy

## Supported Versions

Only the latest release receives security fixes (pre-1.0; minor bumps may break):

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| older   | :x:                |

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Instead, report privately via a
[GitHub private vulnerability report](https://github.com/mytmlt/wrk3/security/advisories/new)
(or email the maintainer if that channel is unavailable).

Code of Conduct reports (non-security) use the same channel — please
prefix the title with `[CoC]` so it routes correctly.

Please include:

- A description of the vulnerability and its impact
- Steps to reproduce (config, commands, environment)
- The `wrk3 version` output and OS/arch

We aim to acknowledge reports within 72 hours and will keep you informed as a
fix is developed. Once fixed, we will publish a GitHub Security Advisory and
credit you (unless you prefer to stay anonymous).

## Scope

`wrk3` executes commands from your own `wrk3.yaml` (`entry.setup/run/stop/logs`)
inside your own checkouts and runs `docker compose` projects on your host.
Treat `wrk3.yaml` like a script: only run configs you trust, and review
`entry.*` commands before `wrk3 up`.

## Sentry DSN

The built-in Sentry DSN is a public client identifier (not a secret) — same
practice as `brew`, `gh`, `sentry-cli`, and frontend JS SDKs. Privacy
controls are on the payload side: the client scrubs paths, branches, emails,
IPs, and secrets before sending, and only sends when explicitly enabled.
`WRK3_SENTRY_DSN` overrides the DSN (forks/self-builds can strip it by
building with an empty DSN).
