# Remote host bootstrap r2 - dev/srv/svc to doctor-green (2026-08-30)

Mission: bootstrap dev, srv, svc to remote-secondmate readiness so persistent
seats can be seeded and launched on them. Seed gate: `fm-remote-doctor.sh`
exits green on each host. All host work done non-interactively
(ssh `-o BatchMode=yes -o ConnectTimeout=6 -o StrictHostKeyChecking=accept-new`,
scp into pre-created dirs, never answered a prompt).

## Method

For each host in order (dev, srv, svc):

1. Verified code root at fleet baseline `1fbc7bb` and ssh access as the host account.
2. Installed herdr + treehouse by copying the gpu reference binaries to
   `/home/<account>/.local/bin/` via scp (`chmod +x`).
3. Installed/completed `tasks-axi` in the npm-global layout (wrapper in
   `~/.local/bin/` per the documented wrapper recipe).
4. Ran `PATH=/home/<account>/.local/bin:$PATH /opt/ra/firstmate/bin/fm-remote-doctor.sh --fix`,
   iterating until `ok: remote second-mate readiness confirmed on this host`.
5. Re-verified through the fixed remote entrypoint protocol (same path the
   seed flow `fm_remote_readiness_ensure` uses: check -> --fix -> check).

## Per host

### dev (account yuan, dev.home.arcs.internal, 192.168.0.6)

- Doctor final: `ok: remote second-mate readiness confirmed on this host` (exit 0),
  both direct and through the entrypoint (`entrypoint=yes`,
  `check entrypoint-link=ok`, `check remote-job-probe=ok`,
  `check herdr-server=ok: session fm-remote is running`).
- herdr 0.8.2 at /home/yuan/.local/bin/herdr; treehouse v2.3.0 at
  /home/yuan/.local/bin/treehouse; tasks-axi 0.2.5 via wrapper ->
  /home/yuan/.npm-global/bin/tasks-axi.
- Deviations from gpu reference layout:
  - dev previously had herdr 0.8.0 in ~/.local/bin (older build); replaced with
    the 0.8.2 reference binary.
  - The prior worker had copied the raw tasks-axi JS entry file over
    ~/.local/bin/tasks-axi (broken: node module resolution failed). Replaced with
    a proper Firstmate-owned wrapper (`#!/usr/bin/env bash` +
    `exec /home/yuan/.npm-global/bin/tasks-axi "$@"`).

### srv (account yuan, srv.home.arcs.internal, 192.168.0.7)

- Doctor final: `ok: remote second-mate readiness confirmed on this host` (exit 0),
  direct and through the entrypoint protocol (`entrypoint=yes`).
- herdr 0.8.2, treehouse v2.3.0 in /home/yuan/.local/bin.
- tasks-axi was absent: mirrored the gpu npm-global package tree
  (/home/yuan/.npm-global/lib/node_modules/tasks-axi, 0.2.5), bin symlink, and a
  Firstmate-owned ~/.local/bin wrapper; runs on linuxbrew node v26.
- `--fix` started the Linux remote-job worker and the fm-remote herdr server;
  created ~/.local/bin/fm-remote-entrypoint.sh symlink; /usr/local/bin symlink
  pre-existed from the earlier bootstrap.
- Services (nginx, vault, portal units) untouched.
- No registry route in data/secondmates.md yet - the main firstmate provisions
  routes at seed time; seed preflight will pass on first check.

### svc (account jon, svc.home.arcs.internal, 192.168.0.8)

- Doctor final: `ok: remote second-mate readiness confirmed on this host` (exit 0),
  direct and through the entrypoint protocol (`entrypoint=yes`).
- herdr 0.8.2, treehouse v2.3.0 in /home/jon/.local/bin.
- tasks-axi was absent: same npm-global mirror + wrapper as srv (0.2.5, node v26).
- `--fix` started the Linux remote-job worker and the fm-remote herdr server;
  created ~/.local/bin/fm-remote-entrypoint.sh symlink.

## Notes

- treehouse v2.3.0 from gpu is a dynamically linked x86_64 ELF (not static as
  the brief assumed); all three hosts run Ubuntu glibc 2.39 so it executes.
- All three doctor runs also passed the required-tool probe through the running
  worker (each check line re-derived after --fix).
- No blockers encountered; zero secret material handled.