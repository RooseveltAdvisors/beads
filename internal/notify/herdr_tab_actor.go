package notify

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HerdrTabActor resolves an agent's speaking identity from its herdr tab label.
//
// The captain names tabs for purpose ("portal AMD biller", "THE-FM,
// "ASD-STE100"), so inside a named tab an agent's comments should carry that
// label instead of the repository's git user.name - which every clone shares
// and which made a real comment read as a self-comment and silently skip its
// notification (2026-10-05, bead wiseman-1fie).
//
// Returns "" when we are not inside a herdr pane (HERDR_SESSION /
// HERDR_TAB_ID unset - CI, cron, plain shells), when the tab has no label, or
// when herdr cannot be asked; callers fall through to their git identity.
func HerdrTabActor() string {
	session := strings.TrimSpace(os.Getenv("HERDR_SESSION"))
	tab := strings.TrimSpace(os.Getenv("HERDR_TAB_ID"))
	if session == "" || tab == "" {
		return ""
	}
	bin := herdrBin()
	if bin == "" {
		return ""
	}
	// Advisory by design: never block a comment on a slow or dead herdr socket.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--session", session, "tab", "list").Output() //nolint:gosec // G702: HERDR_BIN/session/tab come from this process' own environment, the same trust domain as running herdr at all
	if err != nil {
		return ""
	}
	var payload struct {
		Result struct {
			Tabs []struct {
				TabID string `json:"tab_id"`
				Label string `json:"label"`
			} `json:"tabs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return ""
	}
	for _, t := range payload.Result.Tabs {
		if t.TabID == tab {
			return strings.TrimSpace(t.Label)
		}
	}
	return ""
}

// herdrBin follows the same lookup order the discoverer documents: HERDR_BIN,
// PATH, then the home install.
func herdrBin() string {
	if b := strings.TrimSpace(os.Getenv("HERDR_BIN")); b != "" {
		return b
	}
	if p, err := exec.LookPath("herdr"); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".local", "bin", "herdr")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}
