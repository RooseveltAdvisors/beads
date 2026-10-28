package main

import (
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/seats"
)

// healCallerSeat rebinds this process's seat to its current herdr pane.
// It runs from the command path, not from a timer. Identity comes only from
// the agent's durable state (BEADS_AGENT_STATE holds uuid + rebind token);
// a missing herdr env, a test run, or the absence of durable state is a
// no-op. The tab label and actor name are never resolved into an identity.
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

// healSeatRuntime reconciles live herdr endpoints, then re-registers the
// caller's endpoint under the uuid its durable state carries. Discovery
// failure must not block the command: pass haveLive false and the caller
// still rebinds itself.
func healSeatRuntime(o *notify.Outbox, live []notify.Instance, haveLive bool) {
	if o == nil || os.Getenv("BEADS_TEST_MODE") != "" {
		return
	}
	var state *seats.AgentState
	if path := strings.TrimSpace(os.Getenv(seats.StateEnv)); path != "" {
		// A present but unreadable or corrupt state is a loud signal for the
		// explicit register path; on this best-effort path it only skips the
		// rebind. It must never fall back to name or tab-label guessing.
		if st, err := seats.ReadState(path); err == nil {
			state = &st
		}
	}
	self := notify.Instance{
		Session:        strings.TrimSpace(os.Getenv("HERDR_SESSION")),
		PaneID:         strings.TrimSpace(os.Getenv("HERDR_PANE_ID")),
		AgentSessionID: strings.TrimSpace(os.Getenv("PI_SESSION_FILE")),
	}
	_ = o.Heal(state, self, live, time.Now().UTC(), haveLive)
}
