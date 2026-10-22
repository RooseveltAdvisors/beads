package seats

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSeatRenameChangesOneRow(t *testing.T) {
	r := Registry{}
	now := time.Now().UTC()
	for _, id := range []string{"seat:one", "seat:two"} {
		if err := r.Register(id, []string{id + "-old"}, Endpoint{Session: "agents", PaneID: id}, now); err != nil {
			t.Fatal(err)
		}
	}
	other := r["seat:two"]
	if err := r.Rename("seat:one", []string{"new-name", "old-name"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(other, r["seat:two"]) || len(r) != 2 {
		t.Fatal("rename touched another seat")
	}
	if id, err := r.Resolve("new-name"); err != nil || id != "seat:one" {
		t.Fatalf("resolve: %q %v", id, err)
	}
	if _, err := r.Resolve("seat:one-old"); err == nil {
		t.Fatal("removed alias still resolves")
	}
	if err := r.Rename("seat:one", []string{"seat:two"}, now); err == nil {
		t.Fatal("accepted an ambiguous alias")
	}
}

func TestSeatRestartKeepsIdentity(t *testing.T) {
	r := Registry{}
	now := time.Now().UTC()
	if err := r.Register("seat:one", []string{"worker"}, Endpoint{Session: "s", PaneID: "old", AgentSessionID: "old-session"}, now); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("seat:one", nil, Endpoint{Session: "s2", PaneID: "new", AgentSessionID: "new-session"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if id, err := r.Resolve("worker"); err != nil || id != "seat:one" || len(r) != 1 {
		t.Fatalf("identity churned: %v %v", r, err)
	}
	if r["seat:one"].Endpoint.PaneID != "new" {
		t.Fatal("endpoint did not rebind")
	}
	dir := t.TempDir()
	if err := Save(dir, r); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || !reflect.DeepEqual(loaded, r) {
		t.Fatalf("round trip: %v %v", loaded, err)
	}
}

func TestLockFileWaitsForCurrentWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outbox.lock")
	release, err := LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	time.AfterFunc(100*time.Millisecond, release)
	second, err := LockFile(path)
	if err != nil {
		t.Fatalf("contended writer failed instead of waiting: %v", err)
	}
	second()
}

// TestResolveAdmitsMachineActors pins the due-sweep integration seam: a
// workspace that has registered a seat goes strict, and the product's own
// automation writes under constant machine actors (bd-due-trigger,
// bd-recurrence, bd-defer-wake). Those must resolve to their canonical
// spelling while near names stay refused - admission is an exact set, never a
// pattern.
func TestResolveAdmitsMachineActors(t *testing.T) {
	r := Registry{}
	if err := r.Register("wiseman", []string{"wiseman"}, Endpoint{Session: "wiseman", PaneID: "w1:pP7"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"bd-due-trigger", "bd-recurrence", "bd-defer-wake", "bd-deploy"} {
		got, err := r.Resolve(name)
		if err != nil {
			t.Errorf("Resolve(%q) error = %v, want machine actor admitted", name, err)
			continue
		}
		if got != name {
			t.Errorf("Resolve(%q) = %q, want canonical %q", name, got, name)
		}
	}

	if got, err := r.Resolve("BD-Due-Trigger"); err != nil || got != "bd-due-trigger" {
		t.Errorf("Resolve(BD-Due-Trigger) = %q, %v; want canonical bd-due-trigger", got, err)
	}

	for _, name := range []string{"bd-due-trigge", "bd-due-trigger2", "bd-due-trigger-", "due-trigger", "bd-due"} {
		if got, err := r.Resolve(name); err == nil {
			t.Errorf("Resolve(%q) = %q, want refusal (no pattern admission)", name, got)
		}
	}

	if got, err := r.Resolve("nobody"); err == nil {
		t.Errorf("Resolve(nobody) = %q, want refusal for unknown seat", got)
	}

	var empty Registry
	for _, name := range []string{"bd-due-trigger", "bd-recurrence", "bd-defer-wake", "bd-deploy"} {
		if got, err := empty.Resolve(name); err != nil || got != name {
			t.Errorf("empty-registry Resolve(%q) = %q, %v; want machine actor admitted", name, got, err)
		}
	}
}
