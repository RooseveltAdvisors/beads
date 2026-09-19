package notify

import "strings"

// DeliveryMode is how drain submits a notify prompt into a live harness.
// Follow-up waits until the current run finishes. Steer is injected at the
// next tool boundary and can redirect in-flight work.
type DeliveryMode string

const (
	// ModeFollowUp queues the prompt until the agent finishes its current run.
	ModeFollowUp DeliveryMode = "follow_up"
	// ModeSteer submits with the harness default (Enter). Used when the
	// harness has no follow-up submit key.
	ModeSteer DeliveryMode = "steer"
)

// paste wrappers make a multiline notify body one composer paste instead of
// a sequence of newline submits. Same CSI used by herdr agent prompt when
// the pane has bracketed paste enabled.
const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

// ModeForHarness returns follow_up for harnesses with a verified non-steer
// submit key, and steer for everyone else. Beads does not import firstmate;
// this table is the standalone mapping drain uses.
func ModeForHarness(harness string) DeliveryMode {
	if FollowUpSubmitKey(harness) != "" {
		return ModeFollowUp
	}
	return ModeSteer
}

// FollowUpSubmitKey is the herdr pane send-keys name that queues a follow-up
// on harness, or empty when that harness has no follow-up submit.
//
//	pi / pi-signed: Option/Alt+Enter (Pi app.message.followUp)
//	cursor:         Tab queues after the turn (Agents Window / queue surface)
//	codex:          Tab queues for the next turn
//
// Claude Code has no follow-up submit distinct from steer (open feature
// request); unknown harnesses also return empty.
func FollowUpSubmitKey(harness string) string {
	switch normalizeHarness(harness) {
	case "pi", "pi-signed":
		return "alt+enter"
	case "cursor", "cursor-agent":
		return "tab"
	case "codex":
		return "tab"
	default:
		return ""
	}
}

func normalizeHarness(harness string) string {
	return strings.ToLower(strings.TrimSpace(harness))
}

// deliveryCall is one herdr argv vector, without the binary name.
type deliveryCall struct {
	Args []string
}

// deliveryCalls is the herdr command sequence for one prompt. Follow-up
// pastes then sends the harness follow-up key. Steer uses `agent prompt`
// (text + Enter), which also rejects a blocked agent.
func deliveryCalls(inst Instance, text string, mode DeliveryMode) []deliveryCall {
	key := FollowUpSubmitKey(inst.Harness)
	if mode == ModeFollowUp && key != "" {
		return []deliveryCall{
			{Args: sessionArgs(inst, "pane", "send-text", inst.PaneID, wrapFollowUpPaste(text))},
			{Args: sessionArgs(inst, "pane", "send-keys", inst.PaneID, key)},
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
