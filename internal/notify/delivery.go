package notify

import "strings"

// DeliveryMode is how drain submits a notify prompt into a live harness.
// Follow-up waits until the current run finishes. Steer is injected at the
// next tool boundary and can redirect in-flight work. Hold-until-idle defers
// delivery while the agent is busy until the next turn boundary.
type DeliveryMode string

const (
	// ModeFollowUp queues the prompt until the agent finishes its current run.
	ModeFollowUp DeliveryMode = "follow_up"
	// ModeSteer submits with the harness default (Enter). Used when the agent
	// is idle/done, or when the harness has no non-interrupting submit.
	ModeSteer DeliveryMode = "steer"
	// ModeHoldUntilIdle defers delivery while the agent is mid-turn until
	// the next turn boundary.
	ModeHoldUntilIdle DeliveryMode = "hold_until_idle"
)

// QueueMethod identifies how mid-turn follow-up prompts are submitted.
type QueueMethod string

const (
	// QueueMethodAltEnter sends pane text + Option/Alt+Enter (with ctrl+j fallback).
	QueueMethodAltEnter QueueMethod = "alt+enter"
	// QueueMethodTab sends pane text + Tab.
	QueueMethodTab QueueMethod = "tab"
	// QueueMethodEnter submits with Enter (herdr agent prompt), which queues natively while busy.
	QueueMethodEnter QueueMethod = "enter"
	// QueueMethodHoldUntilIdle defers delivery while mid-turn until target is idle/done.
	QueueMethodHoldUntilIdle QueueMethod = "hold_until_idle"
)

// HarnessProfile defines the delivery profile for an agent harness.
type HarnessProfile struct {
	Harness       string      `json:"harness"`
	QueueMethod   QueueMethod `json:"queue_method"`
	HoldUntilIdle bool        `json:"hold_until_idle"`
	SubmitKey     string      `json:"submit_key,omitempty"`
	Verified      bool        `json:"verified"`
	Notes         string      `json:"notes"`
}

// harnessProfiles is the authoritative delivery profile table.
// Per-harness delivery matrix:
//   - Non-interrupting follow-up submit:
//     pi / pi-signed: Option+Enter queue (pane send-text + send-keys alt+enter, fallback ctrl+j).
//     cursor / cursor-agent: Tab queue (pane send-text + send-keys tab).
//     codex: Tab queue (pane send-text + send-keys tab).
//   - Hold-until-idle for harnesses without verified queue semantics:
//     claude, claude-code, agy, gemini, kimi, omp, muse default to hold-until-idle until live-verified.
var harnessProfiles = []HarnessProfile{
	{
		Harness:       "pi",
		QueueMethod:   QueueMethodAltEnter,
		SubmitKey:     "alt+enter",
		HoldUntilIdle: false,
		Verified:      true,
		Notes:         "Verified live 2026-09-22: Pi app.message.followUp queues via Option+Enter (or ctrl+j fallback) for next turn boundary, never interrupts in-flight work.",
	},
	{
		Harness:       "pi-signed",
		QueueMethod:   QueueMethodAltEnter,
		SubmitKey:     "alt+enter",
		HoldUntilIdle: false,
		Verified:      true,
		Notes:         "Verified live 2026-09-22: Signed Pi variant; same Option+Enter queue semantics as pi.",
	},
	{
		Harness:       "cursor",
		QueueMethod:   QueueMethodTab,
		SubmitKey:     "tab",
		HoldUntilIdle: false,
		Verified:      true,
		Notes:         "Verified live 2026-09-22: Cursor Agents Window / queue surface queues via Tab after the turn.",
	},
	{
		Harness:       "cursor-agent",
		QueueMethod:   QueueMethodTab,
		SubmitKey:     "tab",
		HoldUntilIdle: false,
		Verified:      true,
		Notes:         "Verified live 2026-09-22: Cursor CLI agent harness alias; queues via Tab.",
	},
	{
		Harness:       "codex",
		QueueMethod:   QueueMethodTab,
		SubmitKey:     "tab",
		HoldUntilIdle: false,
		Verified:      true,
		Notes:         "Verified live 2026-09-22: Codex CLI queues for the next turn via Tab.",
	},
	{
		Harness:       "claude",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Claude Code Enter mid-turn is steering; defaults to hold-until-idle.",
	},
	{
		Harness:       "claude-code",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Claude Code variant; defaults to hold-until-idle.",
	},
	{
		Harness:       "agy",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: Antigravity CLI has no verified mid-turn queue key. Defaults to hold-until-idle until live-verified.",
	},
	{
		Harness:       "antigravity",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: Full name alias for agy. Defaults to hold-until-idle until live-verified.",
	},
	{
		Harness:       "gemini",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: Gemini harness has no verified non-interrupting mid-turn queue. Defaults to hold-until-idle until live-verified.",
	},
	{
		Harness:       "kimi",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: Kimi harness has no verified non-interrupting mid-turn queue. Defaults to hold-until-idle until live-verified.",
	},
	{
		Harness:       "omp",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: OMP harness has no verified non-interrupting mid-turn queue. Defaults to hold-until-idle until live-verified.",
	},
	{
		Harness:       "muse",
		QueueMethod:   QueueMethodHoldUntilIdle,
		SubmitKey:     "",
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unverified queue semantics: Muse harness has no verified non-interrupting mid-turn queue. Defaults to hold-until-idle until live-verified.",
	},
}

// ProfileForHarness returns the delivery profile for a harness.
// Unknown harnesses default to HoldUntilIdle: true until live-verified.
func ProfileForHarness(harness string) HarnessProfile {
	h := normalizeHarness(harness)
	for _, p := range harnessProfiles {
		if p.Harness == h {
			return p
		}
	}
	return HarnessProfile{
		Harness:       h,
		QueueMethod:   QueueMethodHoldUntilIdle,
		HoldUntilIdle: true,
		Verified:      false,
		Notes:         "Unknown harness: unverified queue semantics, defaults to hold-until-idle.",
	}
}

// HarnessProfiles returns a copy of the known harness delivery profile table.
func HarnessProfiles() []HarnessProfile {
	out := make([]HarnessProfile, len(harnessProfiles))
	copy(out, harnessProfiles)
	return out
}

// ModeForHarness returns the default delivery mode for harness when mid-turn.
// Harnesses with verified non-interrupting follow-up submit (pi, cursor, codex)
// return ModeFollowUp. Unverified harnesses return ModeHoldUntilIdle.
func ModeForHarness(harness string) DeliveryMode {
	prof := ProfileForHarness(harness)
	if prof.HoldUntilIdle {
		return ModeHoldUntilIdle
	}
	return ModeFollowUp
}

// FollowUpSubmitKey is the herdr pane send-keys name that queues a follow-up
// on harness, or empty when that harness has no key-based follow-up submit.
func FollowUpSubmitKey(harness string) string {
	prof := ProfileForHarness(harness)
	return prof.SubmitKey
}

func normalizeHarness(harness string) string {
	return strings.ToLower(strings.TrimSpace(harness))
}

// paste wrappers make a multiline notify body one composer paste instead of
// a sequence of newline submits. Same CSI used by herdr agent prompt when
// the pane has bracketed paste enabled.
const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// deliveryCall is one herdr argv vector, without the binary name.
type deliveryCall struct {
	Args []string
}

// deliveryCalls is the herdr command sequence for one prompt. Follow-up
// pastes then sends the harness follow-up key. Steer uses `agent prompt`
// (text + Enter), which also rejects a blocked agent.
func deliveryCalls(inst Instance, text string, mode DeliveryMode) []deliveryCall {
	prof := ProfileForHarness(inst.Harness)
	if mode == ModeFollowUp && !prof.HoldUntilIdle {
		if prof.SubmitKey != "" {
			return []deliveryCall{
				{Args: sessionArgs(inst, "pane", "send-text", inst.PaneID, wrapFollowUpPaste(text))},
				{Args: sessionArgs(inst, "pane", "send-keys", inst.PaneID, prof.SubmitKey)},
			}
		}
		// claude natively queues text typed with Enter while busy.
		return []deliveryCall{
			{Args: sessionArgs(inst, "agent", "prompt", inst.PaneID, text)},
		}
	}
	return []deliveryCall{
		{Args: sessionArgs(inst, "agent", "prompt", inst.PaneID, text)},
	}
}

func wrapFollowUpPaste(text string) string {
	return bracketedPasteStart + text + bracketedPasteEnd
}

func sessionArgs(inst Instance, rest ...string) []string {
	args := make([]string, 0, 2+len(rest))
	if strings.TrimSpace(inst.Session) != "" {
		args = append(args, "--session", inst.Session)
	}
	return append(args, rest...)
}
