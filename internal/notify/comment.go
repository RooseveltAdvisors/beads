package notify

import (
	"os"
	"os/exec"
	"strings"
)

// ResolveActor returns the actor identity for notification attribution and self-skip.
// Priority: BEADS_ACTOR env var when set, else git config user.name, else $USER, else "unknown".
func ResolveActor() string {
	if a := strings.TrimSpace(os.Getenv("BEADS_ACTOR")); a != "" {
		return a
	}
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if gitUser := strings.TrimSpace(string(out)); gitUser != "" {
			return gitUser
		}
	}
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	return "unknown"
}

// CommentAssigneeSeat is the exact seat a comment ping should address.
// Unassigned beads and self-comments (actor == assignee) do not ping.
func CommentAssigneeSeat(assignee, author string) (string, bool) {
	seat := strings.TrimSpace(assignee)
	if seat == "" || strings.EqualFold(seat, UnassignedSeat) {
		return "", false
	}
	author = strings.TrimSpace(author)
	if author == "" {
		author = ResolveActor()
	}
	if strings.EqualFold(seat, author) {
		return "", false
	}
	return seat, true
}

// CommentPingTitle is the outbox title for a comment ping.
func CommentPingTitle(issueTitle, author, text string) string {
	title := strings.TrimSpace(issueTitle)
	author = strings.TrimSpace(author)
	preview := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if preview == "" {
		if author == "" {
			return title
		}
		return title + " — comment from " + author
	}
	const max = 80
	if len(preview) > max {
		preview = preview[:max] + "…"
	}
	if author == "" {
		return title + " — " + preview
	}
	if title == "" {
		return author + ": " + preview
	}
	return title + " — " + author + ": " + preview
}
