package dolt

import "testing"

// TestIssueIDShape covers the SHAPE half of ID classification. Shape alone
// never routes a query down the ID predicate — issueops.ResolveIDQuery also
// requires the prefix to exist in the target store — so an ID-shaped ordinary
// word such as "rate-limit" is true here and still searched as free text.
func TestIssueIDShape(t *testing.T) {
	tests := []struct {
		input      string
		wantPrefix string
		want       bool
	}{
		// Issue-ID shape: 1-8 char lowercase prefix, one hyphen, alnum/dot tail.
		{"bd-123", "bd", true},
		{"hq-319", "hq", true},
		{"bd-a3f", "bd", true},
		{"test-1", "test", true},
		{"bd-123.1", "bd", true},   // child ID
		{"bd-123.1.2", "bd", true}, // grandchild ID
		{"BD-ABC123", "bd", true},  // case-folded
		{"rate-limit", "rate", true},

		// Not ID-shaped
		{"bd-wisp-abc", "", false},        // hyphen in the tail
		{"beads-vscode-1", "", false},     // hyphen in the tail
		{"authentication bug", "", false}, // has space
		{"login", "", false},              // no hyphen
		{"", "", false},                   // empty
		{"-abc", "", false},               // starts with hyphen
		{"abc-", "", false},               // ends with hyphen (tail empty)
		{"fix: something", "", false},     // colon
		{"bug_fix", "", false},            // underscore
		{"feature/branch", "", false},     // slash
		{"toolongprefix-1", "", false},    // prefix over 8 characters
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			prefix, got := issueIDShape(tt.input)
			if got != tt.want || prefix != tt.wantPrefix {
				t.Errorf("issueIDShape(%q) = (%q, %v), want (%q, %v)", tt.input, prefix, got, tt.wantPrefix, tt.want)
			}
		})
	}
}
