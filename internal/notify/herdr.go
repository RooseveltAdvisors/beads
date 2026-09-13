package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Instance is one live herdr-hosted agent of any harness (pi, claude, codex, …).
// Herdr detects the harness; beads only needs session + pane to deliver.
type Instance struct {
	Session     string `json:"session"`
	PaneID      string `json:"pane_id"`
	Harness     string `json:"harness"` // herdr "agent" field
	Status      string `json:"status"`
	Title       string `json:"title"`
	Cwd         string `json:"cwd"`
	Focused     bool   `json:"focused"`
	Score       int    `json:"score,omitempty"`
	MatchReason string `json:"match_reason,omitempty"`
}

// HerdrDiscoverer finds live agent instances via herdr session/agent list.
// New harnesses appear automatically once herdr can detect them.
type HerdrDiscoverer struct {
	Bin     string
	Timeout time.Duration
}

func (h HerdrDiscoverer) bin() string {
	if b := strings.TrimSpace(h.Bin); b != "" {
		return b
	}
	if env := strings.TrimSpace(os.Getenv("HERDR_BIN")); env != "" {
		return env
	}
	if p, err := exec.LookPath("herdr"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin", "herdr")
}

func (h HerdrDiscoverer) timeout() time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return 15 * time.Second
}

func (h HerdrDiscoverer) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.timeout())
	defer cancel()
	cmd := exec.CommandContext(ctx, h.bin(), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("herdr %v: %w (%s)", args, err, msg)
		}
		return nil, fmt.Errorf("herdr %v: %w", args, err)
	}
	return out, nil
}

// ListSessions returns names of running herdr sessions (excludes default).
func (h HerdrDiscoverer) ListSessions() ([]string, error) {
	out, err := h.run("session", "list", "--json")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Sessions []struct {
			Name    string `json:"name"`
			Running bool   `json:"running"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("herdr session list json: %w", err)
	}
	var names []string
	for _, s := range payload.Sessions {
		if !s.Running || s.Name == "" || s.Name == "default" {
			continue
		}
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names, nil
}

// ListAllInstances walks every running session and collects agents.
func (h HerdrDiscoverer) ListAllInstances() ([]Instance, error) {
	sessions, err := h.ListSessions()
	if err != nil {
		return nil, err
	}
	var all []Instance
	for _, session := range sessions {
		inst, err := h.listSession(session)
		if err != nil {
			continue
		}
		all = append(all, inst...)
	}
	return all, nil
}

func (h HerdrDiscoverer) listSession(session string) ([]Instance, error) {
	out, err := h.run("--session", session, "agent", "list")
	if err != nil {
		return nil, err
	}
	var payload struct {
		Result struct {
			Agents []struct {
				Agent                 string `json:"agent"`
				AgentStatus           string `json:"agent_status"`
				Cwd                   string `json:"cwd"`
				Focused               bool   `json:"focused"`
				PaneID                string `json:"pane_id"`
				TerminalTitle         string `json:"terminal_title"`
				TerminalTitleStripped string `json:"terminal_title_stripped"`
			} `json:"agents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("herdr agent list %s json: %w", session, err)
	}
	var outI []Instance
	for _, a := range payload.Result.Agents {
		if strings.TrimSpace(a.PaneID) == "" {
			continue
		}
		title := a.TerminalTitleStripped
		if title == "" {
			title = a.TerminalTitle
		}
		outI = append(outI, Instance{
			Session: session,
			PaneID:  a.PaneID,
			Harness: a.Agent,
			Status:  a.AgentStatus,
			Title:   title,
			Cwd:     a.Cwd,
			Focused: a.Focused,
		})
	}
	return outI, nil
}

// ResolveSeat picks the best live instance for an assignee seat.
//
// Scoring (higher wins):
//
//	+100 session name equals seat
//	+40  title token equals seat (e.g. "π - wiseman")
//	+20  cwd path segment equals seat
//	+10  focused
//	+5   idle/done (ready)
//	-5   blocked
//
// Primary policy: single best match. Score 0 means no match.
func ResolveSeat(seat string, instances []Instance) (Instance, bool) {
	seat = strings.TrimSpace(strings.ToLower(seat))
	if seat == "" || seat == UnassignedSeat {
		return Instance{}, false
	}
	best := Instance{}
	bestScore := 0
	for _, inst := range instances {
		score, reason := scoreInstance(seat, inst)
		if score <= 0 {
			continue
		}
		inst.Score = score
		inst.MatchReason = reason
		if score > bestScore {
			bestScore = score
			best = inst
		}
	}
	if bestScore == 0 {
		return Instance{}, false
	}
	return best, true
}

func scoreInstance(seat string, inst Instance) (int, string) {
	var score int
	var reasons []string
	session := strings.ToLower(strings.TrimSpace(inst.Session))
	title := strings.ToLower(inst.Title)
	cwd := strings.ToLower(inst.Cwd)

	if session == seat {
		score += 100
		reasons = append(reasons, "session="+inst.Session)
	}
	if titleHasSeatToken(title, seat) {
		score += 40
		reasons = append(reasons, "title")
	}
	if cwdHasSeatSegment(cwd, seat) {
		score += 20
		reasons = append(reasons, "cwd")
	}
	if score == 0 {
		return 0, ""
	}
	if inst.Focused {
		score += 10
		reasons = append(reasons, "focused")
	}
	switch strings.ToLower(inst.Status) {
	case "idle", "done":
		score += 5
		reasons = append(reasons, inst.Status)
	case "blocked":
		score -= 5
		reasons = append(reasons, "blocked")
	}
	return score, strings.Join(reasons, ",")
}

func titleHasSeatToken(title, seat string) bool {
	title = strings.ToLower(strings.ReplaceAll(title, "—", "-"))
	seat = strings.ToLower(seat)
	// Prefer whole-title / suffix forms herdr uses: "π - wiseman", "portal-ops".
	// Do not split on '-' so multi-segment seats (portal-ops) stay one token.
	if title == seat {
		return true
	}
	for _, sep := range []string{" - ", " · ", " | ", " / ", ": "} {
		if i := strings.LastIndex(title, sep); i >= 0 {
			right := strings.TrimSpace(title[i+len(sep):])
			if right == seat {
				return true
			}
		}
	}
	// Whitespace-separated tokens only (keep hyphens inside a token).
	for _, f := range strings.Fields(title) {
		f = strings.Trim(f, "|,/:·")
		if f == seat {
			return true
		}
	}
	return false
}

func cwdHasSeatSegment(cwd, seat string) bool {
	if cwd == "" {
		return false
	}
	if strings.ToLower(filepath.Base(cwd)) == seat {
		return true
	}
	for _, part := range strings.Split(cwd, string(os.PathSeparator)) {
		if strings.ToLower(part) == seat {
			return true
		}
	}
	return false
}

// Prompt delivers text to the agent in inst's pane. Harness-agnostic.
func (h HerdrDiscoverer) Prompt(inst Instance, text string) error {
	args := make([]string, 0, 8)
	if inst.Session != "" {
		args = append(args, "--session", inst.Session)
	}
	args = append(args, "agent", "prompt", inst.PaneID, text)
	_, err := h.run(args...)
	return err
}

// DefaultPrompt builds the standard beads notify prompt for a record.
// Kind-specific footers teach finish-line commands so harnesses that only
// chat or write status files still learn: done means bd close with a real reason.
func DefaultPrompt(rec Record) string {
	title := rec.Title
	if title == "" {
		title = rec.IssueID
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"BEADS NOTIFY (%s). Bead %s just fired for seat %s: %s\n\n",
		rec.Kind, rec.IssueID, rec.Seat, title,
	)
	b.WriteString(promptPlaybook(rec.Kind, rec.IssueID))
	b.WriteString("\nDo the work yourself. Stamp progress on the bead. Go idle. Do not ask the captain.")
	return b.String()
}

// promptPlaybook is the educational body agents see on every wake.
// Stale-claim / progress kinds get the strongest "you must close via bd" copy.
func promptPlaybook(kind Kind, issueID string) string {
	id := strings.TrimSpace(issueID)
	if id == "" {
		id = "<id>"
	}
	switch kind {
	case KindStaleClaim:
		return fmt.Sprintf(`This bead is still open/in_progress under your seat - finishing in chat or a status file is NOT done in beads.

Finish-line (pick one):
  1) If the work is actually finished:
       bd comment %s "what landed (PR/link/result)"
       bd close %s --reason "short real reason (not the word Closed)"
  2) If still working: leave a delta comment, then continue the task:
       bd comment %s "blocked on X / next is Y"
  3) If you should not own it: bd update %s --assignee <seat> (or unassign) and comment why.

House rule: comment.progress_required - bare bd close fails on assigned work; default reason "Closed" does not count. Escape only if needed: bd close %s --force-no-comment --reason "why".
`, id, id, id, id, id)
	case KindProgress:
		return fmt.Sprintf(`Progress trail missing on an assigned bead (comment.progress_required).

Do this before any close:
  bd comment %s "current state / what changed"
When finished:
  bd close %s --reason "what landed"
Do not only update chat or state/*.status - the bead thread is the handoff.
`, id, id)
	case KindEscalate:
		return fmt.Sprintf(`Escalation wake. Inspect the bead, act, and end in beads:
  bd show %s
  bd comment %s "what you found / did"
  bd close %s --reason "..."   # only if the work is truly finished
`, id, id, id)
	default:
		// due / defer / manual - still teach the finish line without drowning the body.
		return fmt.Sprintf(`When the work is finished, close it in beads (harness-agnostic):
  bd comment %s "what landed"    # optional if --reason is explicit
  bd close %s --reason "short real reason"
Assigned work cannot bare-close (comment.progress_required). Chat-only done is not done.
`, id, id)
	}
}
