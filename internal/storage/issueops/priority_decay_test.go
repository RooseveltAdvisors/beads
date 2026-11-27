package issueops

import (
	"testing"
)

// TestDecayPriorityOnAttention pins the rung math: one rung per call toward
// the P2 floor, nothing below it, nothing at or under it touched.
func TestDecayPriorityOnAttention(t *testing.T) {
	cases := []struct {
		current, want int
		decayed       bool
	}{
		{0, 1, true},
		{1, 2, true},
		{2, 2, false},
		{3, 3, false},
		{4, 4, false},
	}
	for _, c := range cases {
		got, decayed := DecayPriorityOnAttention(c.current)
		if got != c.want || decayed != c.decayed {
			t.Errorf("DecayPriorityOnAttention(%d) = (%d, %v), want (%d, %v)",
				c.current, got, decayed, c.want, c.decayed)
		}
	}
}

// TestApplyDecayOnAttention pins the update-seam policy: real actors get the
// decay folded in, explicit priority writes stand, machine actors are not
// attention, and the floor holds.
func TestApplyDecayOnAttention(t *testing.T) {
	up := func() map[string]interface{} { return map[string]interface{}{"title": "x"} }

	m := up()
	ApplyDecayOnAttention(m, 0, "live-seat")
	if m["priority"] != 1 {
		t.Errorf("P0 + real actor: priority = %v, want 1", m["priority"])
	}
	m = up()
	ApplyDecayOnAttention(m, 1, "live-seat")
	if m["priority"] != 2 {
		t.Errorf("P1 + real actor: priority = %v, want 2", m["priority"])
	}
	m = up()
	ApplyDecayOnAttention(m, 0, "bd-due-trigger")
	if _, injected := m["priority"]; injected {
		t.Error("machine actor decayed; machinery is not attention")
	}
	m = up()
	m["priority"] = 0
	ApplyDecayOnAttention(m, 1, "live-seat")
	if m["priority"] != 0 {
		t.Errorf("explicit priority write decayed to %v; the operator's word stands", m["priority"])
	}
	m = up()
	ApplyDecayOnAttention(m, 2, "live-seat")
	if _, injected := m["priority"]; injected {
		t.Error("P2 decayed below the floor")
	}
}
