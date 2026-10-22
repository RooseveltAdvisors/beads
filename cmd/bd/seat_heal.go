package main

import (
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/notify"
)

// healCallerSeat rebinds this process's seat to its current herdr pane.
// It runs from the command path, not from a timer. A missing herdr env, a
// test run, or an unknown tab label is a no-op.
func healCallerSeat() {
	if os.Getenv("BEADS_TEST_MODE") != "" {
		return
	}
	if strings.TrimSpace(os.Getenv("HERDR_SESSION")) == "" || strings.TrimSpace(os.Getenv("HERDR_PANE_ID")) == "" {
		return
	}
	dir := beads.FindBeadsDir()
	if dir == "" {
		return
	}
	o, err := notify.Open(dir)
	if err != nil {
		return
	}
	healSeatRuntime(o, nil, false)
}

// healSeatRuntime reconciles live herdr endpoints, then rebinds the caller.
// Discovery failure must not block the command: pass haveLive false and the
// caller still rebinds itself.
func healSeatRuntime(o *notify.Outbox, live []notify.Instance, haveLive bool) {
	if o == nil || os.Getenv("BEADS_TEST_MODE") != "" {
		return
	}
	name := strings.TrimSpace(os.Getenv("BEADS_ACTOR"))
	if name == "" {
		name = notify.HerdrTabActor()
	}
	self := notify.Instance{
		Session:        strings.TrimSpace(os.Getenv("HERDR_SESSION")),
		PaneID:         strings.TrimSpace(os.Getenv("HERDR_PANE_ID")),
		AgentSessionID: strings.TrimSpace(os.Getenv("PI_SESSION_FILE")),
	}
	_ = o.Heal(name, self, live, time.Now().UTC(), haveLive)
}
