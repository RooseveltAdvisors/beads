package seats

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestMintMintsImmutableUUIDGuardedByToken(t *testing.T) {
	r := Registry{}
	now := time.Now().UTC()
	id, token, err := r.Mint([]string{"worker"}, Endpoint{Session: "s", PaneID: "p1", AgentSessionID: "session-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("minted id %q is not a uuid: %v", id, err)
	}
	if token == "" || r[id].TokenSHA256 == "" || r[id].TokenSHA256 == token {
		t.Fatal("mint must return a bearer token and store only its hash")
	}

	// The unauthenticated register path refuses a token-protected seat.
	if err := r.Register(id, nil, Endpoint{Session: "s2", PaneID: "evil"}, now); err == nil {
		t.Fatal("token-protected seat accepted an unauthenticated re-register")
	}
	// A wrong token refuses and moves nothing.
	if err := r.RebindWithToken(id, Endpoint{Session: "s2", PaneID: "evil"}, "wrong", now); err == nil {
		t.Fatal("wrong token accepted")
	}
	if r[id].Endpoint.PaneID != "p1" {
		t.Fatalf("refused rebind moved the endpoint to %s", r[id].Endpoint.PaneID)
	}
	// The right token re-asserts the endpoint; uuid and aliases never change.
	if err := r.RebindWithToken(id, Endpoint{Session: "s2", PaneID: "p2", AgentSessionID: "session-2"}, token, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if r[id].Endpoint.PaneID != "p2" || len(r[id].Aliases) != 1 || r[id].Aliases[0] != "worker" {
		t.Fatalf("rebind churned identity: %+v", r[id])
	}
	// The uuid survives a save/load round trip with its protection intact.
	dir := t.TempDir()
	if err := Save(dir, r); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil || !reflect.DeepEqual(loaded, r) {
		t.Fatalf("round trip: %v %v", loaded, err)
	}
	// Re-registration never invents an identity.
	if err := r.RebindWithToken(uuid.NewString(), Endpoint{Session: "s", PaneID: "p"}, token, now); err == nil {
		t.Fatal("unknown uuid accepted at rebind")
	}
}

func TestAgentStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agent-state.json")
	st := AgentState{AgentUUID: uuid.NewString(), Alias: "worker", RebindToken: "tok"}
	if err := WriteState(path, st); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o, want 0600 (it carries a bearer token)", info.Mode().Perm())
	}
	got, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentUUID != st.AgentUUID || got.RebindToken != st.RebindToken || got.MintedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := ReadState(filepath.Join(t.TempDir(), "absent.json")); !os.IsNotExist(err) {
		t.Fatalf("missing state error = %v, want os.ErrNotExist", err)
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadState(corrupt); err == nil || os.IsNotExist(err) {
		t.Fatalf("corrupt state error = %v, want a refusal that is not not-exist", err)
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

// TestDeleteRefusesLiveAndFreesName pins the delete contract: only dead rows
// go, and a deleted legacy row's name is free for a mint with that alias.
func TestDeleteRefusesLiveAndFreesName(t *testing.T) {
	now := time.Now().UTC()
	r := Registry{}
	endpoint := Endpoint{Session: "s", PaneID: "w1:p1", AgentSessionID: "/tmp/a.jsonl"}
	if err := r.Register("legacy-seat", []string{"beads"}, endpoint, now); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := r.Delete("legacy-seat"); err == nil {
		t.Error("delete of a live seat parsed, want the liveness refusal")
	}
	if err := r.Delete("missing"); err == nil {
		t.Error("delete of an unknown id parsed, want unknown seat id")
	}
	// Unbind marks dead without dropping identity; delete then removes it.
	seat := r["legacy-seat"]
	seat.Liveness = Dead
	r["legacy-seat"] = seat
	if err := r.Delete("legacy-seat"); err != nil {
		t.Fatalf("delete of a dead seat: %v", err)
	}
	if _, ok := r["legacy-seat"]; ok {
		t.Error("row still present after delete")
	}
	// The freed alias now mints.
	id, _, err := r.Mint([]string{"beads"}, endpoint, now)
	if err != nil {
		t.Fatalf("mint after delete: %v", err)
	}
	if id == "legacy-seat" {
		t.Error("mint returned the legacy name as the id, want a uuid")
	}
}
