package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// repeatPeriod must price close-spawn cadences the way the fleet reads them:
// a */10 heartbeat means a new identical row every 10 minutes (144/day), which
// is the duplicate-bead growth measured on seat billing-learn (2026-10-05).
func TestRepeatPeriodMeasuresCloseSpawnCadence(t *testing.T) {
	cases := []struct {
		pattern string
		want    time.Duration
	}{
		{"*/10 * * * *", 10 * time.Minute},
		{"0 * * * *", time.Hour},
		{"+1d", 24 * time.Hour},
	}
	for _, c := range cases {
		got, ok := repeatPeriod(c.pattern)
		if !ok {
			t.Fatalf("repeatPeriod(%q) reported no occurrence", c.pattern)
		}
		if got < c.want-time.Second || got > c.want+time.Second {
			t.Fatalf("repeatPeriod(%q) = %s, want %s", c.pattern, got, c.want)
		}
	}
}

func TestWarnSubHourlyRepeatOnlyWarnsBelowAnHour(t *testing.T) {
	capture := func(pattern string) string {
		old := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stderr = w
		func() {
			defer func() { os.Stderr = old }()
			warnSubHourlyRepeat(pattern)
		}()
		_ = w.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		_ = r.Close()
		return buf.String()
	}

	if out := capture("*/10 * * * *"); !bytes.Contains([]byte(out), []byte("144 identical beads/day")) {
		t.Fatalf("*/10 must warn with the daily row cost, got %q", out)
	}
	if out := capture("0 * * * *"); out != "" {
		t.Fatalf("hourly cadence must stay silent, got %q", out)
	}
	if out := capture("+1d"); out != "" {
		t.Fatalf("daily cadence must stay silent, got %q", out)
	}
}
