# Harness parity report — 2026-08-30

Scope: harness tools only. No nginx, vault, portal, or other services were
changed.

## Bare-shell resolution

Validated with an empty environment and `PATH=/usr/local/bin:/usr/bin:/bin`.
Every host resolves all seven commands through the same compatibility path:

| Tool | dev (`yuan`) | srv (`yuan`) | svc (`jon`) |
| --- | --- | --- | --- |
| herdr | `/usr/local/bin/herdr` | `/usr/local/bin/herdr` | `/usr/local/bin/herdr` |
| treehouse | `/usr/local/bin/treehouse` | `/usr/local/bin/treehouse` | `/usr/local/bin/treehouse` |
| tasks-axi | `/usr/local/bin/tasks-axi` | `/usr/local/bin/tasks-axi` | `/usr/local/bin/tasks-axi` |
| cursor-agent | `/usr/local/bin/cursor-agent` | `/usr/local/bin/cursor-agent` | `/usr/local/bin/cursor-agent` |
| bd | `/usr/local/bin/bd` | `/usr/local/bin/bd` | `/usr/local/bin/bd` |
| claude | `/usr/local/bin/claude` | `/usr/local/bin/claude` | `/usr/local/bin/claude` |
| codex | `/usr/local/bin/codex` | `/usr/local/bin/codex` | `/usr/local/bin/codex` |

The harness links resolve to the account-local installs:

| Tool | dev/srv target | svc target |
| --- | --- | --- |
| herdr | `/home/yuan/.local/bin/herdr` | `/home/jon/.local/bin/herdr` |
| treehouse | `/home/yuan/.local/bin/treehouse` | `/home/jon/.local/bin/treehouse` |
| tasks-axi | `/home/yuan/.npm-global/lib/node_modules/tasks-axi/dist/bin/tasks-axi.js` | `/home/jon/.npm-global/lib/node_modules/tasks-axi/dist/bin/tasks-axi.js` |
| cursor-agent | `/home/yuan/.local/share/cursor-agent/versions/2026.08.25-3e8eec8/cursor-agent` | `/home/jon/.local/share/cursor-agent/versions/2026.08.25-3e8eec8/cursor-agent` |
| bd | `/home/yuan/.local/bin/bd` | `/home/jon/.local/bin/bd` |

Reference versions validated on all hosts: herdr `0.8.2`, treehouse `v2.3.0`,
tasks-axi `0.2.5`, cursor-agent `2026.08.25-3e8eec8`, and bd `1.2.2`.
The cursor versions directory has 979 files and manifest digest
`49a69f2f1eb8e0301b74c14e01fd02338a3089df1eae5e442ffdead2c196abf8` on gpu,
dev, srv, and svc.

## Doctor validation

`/opt/ra/firstmate/bin/fm-on.sh <host> fm-remote-doctor.sh` exited 0 on every
host. Each reported:

`ok: remote second-mate readiness confirmed on this host`

Entrypoint checks also passed (`entrypoint=yes`, `check entrypoint-link=ok`, and
the remote worker required-tool probe was ok).

## Deviations

- claude remains the existing host installation and resolves through the
  standardized `/usr/local/bin/claude` link; it reports `2.1.170` everywhere.
- codex remains the existing host installation and now has a `/usr/local/bin`
  compatibility link. Its host-provided versions differ: dev `0.98.0`, srv
  `0.137.0`, svc `0.104.0`. No codex package was upgraded.
- The doctor-managed `~/.local/bin/tasks-axi` wrapper remains on each host and
  delegates to the matching npm-global `tasks-axi` package.
- Dev had an unused `2025.09.18-7ae6800` cursor version; it was removed. The
  remaining cursor payload now matches gpu exactly. This is recoverable only
  from the gpu reference copy; it was not archived locally.
- Optional doctor tools differ by host and were outside this task's scope.
