package notify

import "strings"

// CommentAssigneeSeat is the exact seat a comment ping should address.
// Unassigned beads and self-comments do not ping.
func CommentAssigneeSeat(assignee, author string) (string, bool) {
	seat := strings.TrimSpace(assignee)
	if seat == "" || strings.EqualFold(seat, UnassignedSeat) {
		return "", false
	}
	if strings.EqualFold(seat, strings.TrimSpace(author)) {
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
