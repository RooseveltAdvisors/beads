# A clock for due beads

Beads fires due beads lazily, on ready-work reads. That is a latency floor, not
a clock: a workspace nobody reads never fires anything, and a deadline nobody
hears about is not a deadline. These units are the clock.

A `systemd --user` timer runs `bd due sweep` on a fixed cadence. Each sweep
records a `due` audit event for every bead whose deadline has arrived, moves
that deadline forward so it nags again instead of going silent, counts the
miss, and raises the bead's priority once at the third miss. The script then
publishes a one-line summary wherever your site already looks.

## Why a timer rather than a daemon or an existing poller

A daemon is a second thing to supervise, and a clock that needs its own
supervisor has moved the problem rather than solved it. `systemd --user` is
already supervised, already restarts, already logs, and `Persistent=true`
already owes you the ticks it missed while the machine was asleep.

Reusing an existing poller looks cheaper and is not: that poller's outages
become the clock's outages, silently, and nothing distinguishes "no beads were
due" from "nothing has run for nine hours". The heartbeat below is what closes
that gap, and it belongs to the thing that actually sweeps.

## Install

```sh
# 1. The sweep script, and a publisher if you want one.
mkdir -p ~/.local/libexec
install -m 0755 bd-due-sweep.sh ~/.local/libexec/

# 2. The units.
mkdir -p ~/.config/systemd/user
install -m 0644 bd-due-sweep.service bd-due-sweep.timer \
                bd-due-sweep-failure.service ~/.config/systemd/user/

# 3. Point it at the workspace (and anything else) with a drop-in.
systemctl --user edit bd-due-sweep.service

# 4. Keep the timer running when nobody is logged in. Without this the user
#    manager stops at logout and the clock stops with it.
loginctl enable-linger "$USER"

systemctl --user daemon-reload
systemctl --user enable --now bd-due-sweep.timer
```

Verify:

```sh
systemctl --user list-timers bd-due-sweep.timer
systemctl --user start bd-due-sweep.service   # run one sweep now
journalctl --user -u bd-due-sweep.service -n 20
cat "${XDG_STATE_HOME:-$HOME/.local/state}/bd-due-sweep/last-sweep"
```

## Watching the clock

The script stamps `last-sweep` **after** a sweep completes and publishes, so
the file's age is the honest answer to "when did this last work". A clock that
cannot say when it last ran is a clock nobody can trust, and the two failures
that matter most are the quiet ones:

- **The sweep fails.** `OnFailure=` fires `bd-due-sweep-failure.service`.
  Replace its `ExecStart` with whatever your site pages on.
- **The timer stops firing at all.** Nothing inside this machine can report
  that reliably — a stopped clock has no way to announce its own silence. Check
  `last-sweep`'s age from an **independent host** and alert when it exceeds a
  few intervals. The timer watches the beads; something else watches the timer.

Overlapping ticks are not a failure: the script takes a non-blocking `flock`
and skips a tick whose predecessor is still running, so a slow sweep can never
stack a queue of sweeps on one database.

## Publishing the summary

With no `BD_DUE_SWEEP_PUBLISH` set, the summary goes to stdout and the journal
keeps it. Set the variable to any command and it is called with the summary as
its single argument.

`publish-wake-firstmate.sh` is a worked example for a firstmate home: it sources
`fm-wake-lib.sh` and calls `fm_wake_append`. Publish through the owning helper
like this rather than appending to a queue file directly — the helper
serializes on the queue lock, allocates the sequence number, and writes the
recovery marker the consumer's acknowledgement depends on. A raw append skips
all three and corrupts the rail for every other producer.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `BD_DUE_SWEEP_WORKSPACE` | *(required)* | Directory holding the `.beads` workspace |
| `BD_DUE_SWEEP_BD` | `bd` | Path to the `bd` binary |
| `BD_DUE_SWEEP_TIMEOUT` | `120` | Seconds one sweep may take |
| `BD_DUE_SWEEP_STATE` | `$XDG_STATE_HOME/bd-due-sweep` | Lock and heartbeat directory |
| `BD_DUE_SWEEP_PUBLISH` | *(none)* | Command receiving the summary as `$1` |

Run the sweep against the database directly. `bd due sweep` refuses
proxied-server mode: the server already sweeps on its own reads, so a proxied
client asking for one is asking the wrong process.
