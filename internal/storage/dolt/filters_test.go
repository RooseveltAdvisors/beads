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
		// Issue-ID shape: a prefix, the LAST hyphen, an alnum/dot tail.
		{"bd-123", "bd", true},
		{"hq-319", "hq", true},
		{"bd-a3f", "bd", true},
		{"test-1", "test", true},
		{"bd-123.1", "bd", true},   // child ID
		{"bd-123.1.2", "bd", true}, // grandchild ID
		{"BD-ABC123", "BD", true},  // case preserved for a case-sensitive LIKE
		{"rate-limit", "rate", true},
		{"bd-wisp-abc", "bd-wisp", true},         // last hyphen separates
		{"beads-vscode-1", "beads-vscode", true}, // hyphenated prefix
		{"toolongprefix-1", "toolongprefix", true},
		{"bug_fix-1", "bug_fix", true}, // underscore prefix

		// Not ID-shaped
		{"authentication bug", "", false}, // has space
		{"login", "", false},              // no hyphen
		{"", "", false},                   // empty
		{"-abc", "", false},               // starts with hyphen
		{"abc-", "", false},               // ends with hyphen (tail empty)
		{"fix: something", "", false},     // colon
		{"feature/branch", "", false},     // slash
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
