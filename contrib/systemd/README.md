# A clock for due beads

One `systemd --user` timer. One script. Fire dues, deliver seat notifies.

```
bd-due-sweep.timer
  → bd-due-sweep.sh
       → bd due sweep --json     # timekeeping + outbox enqueue
       → bd notify drain --all   # herdr resolve + prompt
       → stamp last-sweep
```

## Install

```sh
mkdir -p ~/.local/libexec ~/.config/systemd/user
install -m 0755 bd-due-sweep.sh ~/.local/libexec/
install -m 0644 bd-due-sweep.service bd-due-sweep.timer \
                bd-due-sweep-failure.service ~/.config/systemd/user/

systemctl --user edit bd-due-sweep.service   # set WORKSPACE + BD binary
loginctl enable-linger "$USER"
systemctl --user daemon-reload
systemctl --user enable --now bd-due-sweep.timer
```

Drop-in example:

```ini
[Service]
Environment=BD_DUE_SWEEP_WORKSPACE=/path/to/workspace
Environment=BD_DUE_SWEEP_BD=%h/.local/bin/bd
Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
Environment=HERDR_BIN=%h/.local/bin/herdr
```

## What you do not need

- **Second notify timer** - drain runs in the sweep tick. Offline agents retry next sweep.
- **publish-wake-firstmate.sh** - optional legacy summary line only; seat delivery is `bd notify`, not FM queue.
- **Per-seat special cases** - assignee → herdr discover → prompt. Any harness herdr detects.

## Manual drain / inspect

```sh
bd notify pending
bd notify resolve --seat some-assignee
bd notify drain --all
```

## Watching the clock

`last-sweep` mtime is "when the clock last completed". Alert from another host if stale.
`OnFailure=` → `bd-due-sweep-failure.service` for hard failures.
