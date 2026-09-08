//go:build cgo

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEmbeddedRecurringTaskRoundTripAndOwnerLifecycle(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt recurrence tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "rec")
	created := bdCreate(t, bd, dir,
		"Publish daily jonroosevelt.com blog post",
		"--assignee", "jr-voice",
		"--repeat", "0 9 * * *",
		"--recurrence-start", "2026-09-08",
		"--recurrence-tz", "America/New_York",
	)

	assertRecurringBlog := func(raw string, status string) {
		t.Helper()
		var details struct {
			Assignee string            `json:"assignee"`
			Status   string            `json:"status"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(parseShowJSON(t, raw), &details); err != nil {
			t.Fatal(err)
		}
		if details.Assignee != "jr-voice" || details.Status != status {
			t.Fatalf("owner/status = %q/%q, want jr-voice/%s", details.Assignee, details.Status, status)
		}
		want := map[string]string{
			"repeat":           "0 9 * * *",
			"recurrence_start": "2026-09-08",
			"recurrence_tz":    "America/New_York",
		}
		for key, value := range want {
			if details.Metadata[key] != value {
				t.Fatalf("metadata[%q] = %q, want %q", key, details.Metadata[key], value)
			}
		}
		if _, ok := details.Metadata["recurrence_end"]; ok {
			t.Fatal("recurrence_end should be absent")
		}
	}

	assertRecurringBlog(bdShowJSON(t, bd, dir, created.ID), "open")
	for _, args := range [][]string{
		{"--actor", "jr_voice", "update", created.ID, "--claim"},
		{"--actor", "jr_voice", "close", created.ID, "--reason", "daily occurrence recorded"},
	} {
		cmd := exec.Command(bd, args...)
		cmd.Dir = dir
		cmd.Env = bdEnv(dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bd %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	assertRecurringBlog(bdShowJSON(t, bd, dir, created.ID), "closed")
}

func TestEmbeddedRecurringTaskValidation(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt recurrence tests")
	}
	t.Parallel()

	bd := buildEmbeddedBD(t)
	dir, _, _ := bdInit(t, bd, "--prefix", "recv")
	base := []string{"Recurring", "--assignee", "jr-voice", "--recurrence-start", "2026-09-08", "--recurrence-tz", "UTC"}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "schedule", args: append(append([]string{}, base...), "--repeat", "61 9 * * *"), want: "invalid recurrence schedule"},
		{name: "timezone", args: []string{"Recurring", "--assignee", "jr-voice", "--repeat", "daily", "--recurrence-start", "2026-09-08", "--recurrence-tz", "Mars/Olympus"}, want: "invalid recurrence timezone"},
		{name: "bounds", args: append(append([]string{}, base...), "--repeat", "daily", "--recurrence-end", "2026-09-07"), want: "is before recurrence_start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := bdCreateFail(t, bd, dir, tc.args...)
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output missing %q:\n%s", tc.want, out)
			}
		})
	}

	created := bdCreate(t, bd, dir,
		"Editable recurring task",
		"--assignee", "pi-enforcement",
		"--repeat", "weekly",
		"--recurrence-start", "2026-09-08",
		"--recurrence-tz", "UTC",
	)
	out := bdUpdateFail(t, bd, dir, created.ID, "--recurrence-end", "2026-09-07")
	if !strings.Contains(out, "is before recurrence_start") {
		t.Fatalf("update output missing bounds error:\n%s", out)
	}
	bdUpdate(t, bd, dir, created.ID, "--recurrence-end", "2026-12-31")
	var details struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(parseShowJSON(t, bdShowJSON(t, bd, dir, created.ID)), &details); err != nil {
		t.Fatal(err)
	}
	if details.Metadata["recurrence_end"] != "2026-12-31" {
		t.Fatalf("recurrence_end did not round trip through update: %v", details.Metadata)
	}
	bdUpdate(t, bd, dir, created.ID, "--repeat=")
	var oneOff struct {
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(parseShowJSON(t, bdShowJSON(t, bd, dir, created.ID)), &oneOff); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"repeat", "recurrence_start", "recurrence_end", "recurrence_tz"} {
		if _, ok := oneOff.Metadata[key]; ok {
			t.Fatalf("%s survived --repeat='' removal: %v", key, oneOff.Metadata)
		}
	}
}
