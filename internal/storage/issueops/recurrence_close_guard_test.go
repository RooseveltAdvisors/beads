package issueops

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/types"
)

func withRecurrenceCloseGuard(t *testing.T, enabled bool) {
	t.Helper()
	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	config.Set(RecurrenceCloseGuardKey, enabled)
	t.Cleanup(config.ResetForTesting)
	if got := RecurrenceCloseGuardEnabled(); got != enabled {
		t.Fatalf("RecurrenceCloseGuardEnabled() = %v, want %v", got, enabled)
	}
}

func TestRecurrenceCloseGuardDefaultsOn(t *testing.T) {
	t.Chdir(t.TempDir())
	config.ResetForTesting()
	if err := config.Initialize(); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	t.Cleanup(config.ResetForTesting)

	if !RecurrenceCloseGuardEnabled() {
		t.Fatal("RecurrenceCloseGuardEnabled() = false, want default true")
	}
}

func TestRecurrenceCloseGuardOnWithoutConfigInitialize(t *testing.T) {
	config.ResetForTesting()
	t.Cleanup(config.ResetForTesting)

	if !RecurrenceCloseGuardEnabled() {
		t.Fatal("RecurrenceCloseGuardEnabled() = false without config.Initialize, want true")
	}
	if err := ValidateRecurrenceSpawn(&types.Issue{ID: "bd-lib", RepeatPattern: "*/10 * * * *"}); err == nil {
		t.Fatal("ValidateRecurrenceSpawn() = nil without config.Initialize, want refusal")
	}
}

func TestValidateRecurrenceSpawn_NilOrNonRecurring(t *testing.T) {
	withRecurrenceCloseGuard(t, true)

	if err := ValidateRecurrenceSpawn(nil); err != nil {
		t.Errorf("ValidateRecurrenceSpawn(nil) = %v, want nil", err)
	}

	nonRecurring := &types.Issue{ID: "bd-plain", RepeatPattern: ""}
	if err := ValidateRecurrenceSpawn(nonRecurring); err != nil {
		t.Errorf("ValidateRecurrenceSpawn(nonRecurring) = %v, want nil", err)
	}
}

// Scope: ALL recurring issues, NO cadence floor. A 2h recurring close must be
// REFUSED too (as well as 1d, 1w, 10m, cron).
func TestValidateRecurrenceSpawn_RefusesAllRecurring(t *testing.T) {
	withRecurrenceCloseGuard(t, true)

	cadences := []struct {
		name    string
		pattern string
	}{
		{"sub-hourly cron */10", "*/10 * * * *"},
		{"hourly interval +1h", "+1h"},
		{"two-hour interval +2h", "+2h"},
		{"daily interval +1d", "+1d"},
		{"weekly interval +1w", "+1w"},
		{"weekly cron", "0 9 * * 1"},
	}

	for _, tc := range cadences {
		t.Run(tc.name, func(t *testing.T) {
			issue := &types.Issue{
				ID:            "bd-rec-1",
				RepeatPattern: tc.pattern,
			}
			err := ValidateRecurrenceSpawn(issue)
			if err == nil {
				t.Fatalf("ValidateRecurrenceSpawn(%s) = nil, want refusal error", tc.pattern)
			}
		})
	}
}

func TestValidateRecurrenceSpawn_ErrorMessageContent(t *testing.T) {
	withRecurrenceCloseGuard(t, true)

	issue := &types.Issue{
		ID:            "bd-test-err",
		RepeatPattern: "+1d",
	}
	err := ValidateRecurrenceSpawn(issue)
	if err == nil {
		t.Fatal("ValidateRecurrenceSpawn = nil, want error")
	}
	msg := err.Error()

	requiredSubstrings := []string{
		"recurrence.close_guard: bd-test-err is a recurring bead",
		"files a NEW row for its successor",
		"Work on the SAME bead: comment your result and leave it open",
		"the due sweep re-dates it and re-fires it",
		`bd update bd-test-err --repeat ""`,
		"bd close --force does not bypass this gate",
		"bd config set recurrence.close_guard false",
	}

	for _, sub := range requiredSubstrings {
		if !strings.Contains(msg, sub) {
			t.Errorf("error message missing expected substring %q\nFull message:\n%s", sub, msg)
		}
	}
}

func TestValidateRecurrenceSpawn_DisabledByConfig(t *testing.T) {
	withRecurrenceCloseGuard(t, false)

	for _, pattern := range []string{"*/10 * * * *", "+2h", "+1d", "+1w"} {
		issue := &types.Issue{
			ID:            "bd-rec-allowed",
			RepeatPattern: pattern,
		}
		if err := ValidateRecurrenceSpawn(issue); err != nil {
			t.Errorf("ValidateRecurrenceSpawn with guard disabled (%s) = %v, want nil", pattern, err)
		}
	}
}

func TestValidateRecurrenceSpawn_ClearedRepeatAllowsClose(t *testing.T) {
	withRecurrenceCloseGuard(t, true)

	// Series cleared with: bd update <id> --repeat ""
	issue := &types.Issue{
		ID:            "bd-rec-cleared",
		RepeatPattern: "",
	}
	if err := ValidateRecurrenceSpawn(issue); err != nil {
		t.Errorf("ValidateRecurrenceSpawn on cleared repeat = %v, want nil", err)
	}
}

func TestRecurrenceCloseHint(t *testing.T) {
	t.Run("nil or non-recurring returns empty", func(t *testing.T) {
		withRecurrenceCloseGuard(t, true)
		if got := RecurrenceCloseHint(nil); got != "" {
			t.Errorf("RecurrenceCloseHint(nil) = %q, want empty", got)
		}
		if got := RecurrenceCloseHint(&types.Issue{}); got != "" {
			t.Errorf("RecurrenceCloseHint(plain) = %q, want empty", got)
		}
	})

	t.Run("disabled guard returns empty", func(t *testing.T) {
		withRecurrenceCloseGuard(t, false)
		issue := &types.Issue{ID: "bd-rec", RepeatPattern: "+1d"}
		if got := RecurrenceCloseHint(issue); got != "" {
			t.Errorf("RecurrenceCloseHint(disabled) = %q, want empty", got)
		}
	})

	t.Run("recurring bead with guard enabled returns hint", func(t *testing.T) {
		withRecurrenceCloseGuard(t, true)
		issue := &types.Issue{ID: "bd-hint-1", RepeatPattern: "+1d"}
		hint := RecurrenceCloseHint(issue)
		if hint == "" {
			t.Fatal("RecurrenceCloseHint = empty, want non-empty hint")
		}

		required := []string{
			"Recurrence note (bd-hint-1): work on THIS bead",
			"comment your result and leave it open",
			"bd refuses to close it (recurrence.close_guard)",
			"the due sweep re-dates it and re-fires it",
			`bd update bd-hint-1 --repeat ""`,
		}
		for _, s := range required {
			if !strings.Contains(hint, s) {
				t.Errorf("RecurrenceCloseHint missing %q\nFull hint:\n%s", s, hint)
			}
		}
	})
}
