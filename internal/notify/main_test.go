package notify

import (
	"os"
	"testing"
)

// TestMain keeps the suite herdr-independent. The actor chain reads
// HERDR_TAB_ID/HERDR_SESSION so an agent inside a named tab speaks as that
// label; a test process run from such a tab would otherwise leak the label
// into actor assertions (and pass or fail depending on where it was launched).
// Tests that need the lookup set the variables themselves with t.Setenv.
func TestMain(m *testing.M) {
	for _, key := range []string{
		"HERDR_PANE_ID", "HERDR_SESSION", "HERDR_TAB_ID", "HERDR_ENV", "HERDR_SOCKET_PATH", "HERDR_BIN",
	} {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
