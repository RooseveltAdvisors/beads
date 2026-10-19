// Package seats owns the exact name -> immutable identity -> endpoint lookup.
// Names and endpoints may change. Only registry keys are persisted as identities.
package seats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const File = "seats.json"

const (
	Live  = "live"
	Stale = "stale"
	Dead  = "dead"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9_:\-]+$`)

// Endpoint is an opaque exact locator, never derived from an alias or ID.
type Endpoint struct {
	Session        string `json:"session,omitempty"`
	PaneID         string `json:"pane_id,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
}

type Seat struct {
	Aliases      []string   `json:"aliases"`
	Session      string     `json:"session,omitempty"`
	Endpoint     Endpoint   `json:"endpoint"`
	Liveness     string     `json:"liveness"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastSeenAt   time.Time  `json:"last_seen_at,omitempty"`
	MissingSince *time.Time `json:"missing_since,omitempty"`
}

type Registry map[string]Seat

type BackfillRow struct {
	Table   string `json:"table"`
	IssueID string `json:"issue_id"`
	Field   string `json:"field"`
	Name    string `json:"name"`
	SeatID  string `json:"seat_id,omitempty"`
	Action  string `json:"action"`
}

func ValidateID(id string) error {
	if !validID.MatchString(id) || strings.EqualFold(id, "unassigned") {
		return fmt.Errorf("invalid seat id %q: use letters, digits, _, :, or -", id)
	}
	return nil
}

func (r Registry) Validate() error {
	seen := map[string]string{}
	for id, seat := range r {
		if err := ValidateID(id); err != nil {
			return err
		}
		for _, name := range append([]string{id}, seat.Aliases...) {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" || key == "unassigned" {
				return fmt.Errorf("seat %s has an empty or reserved alias", id)
			}
			if prev, ok := seen[key]; ok && prev != id {
				return fmt.Errorf("ambiguous seat name %q belongs to %s and %s", name, prev, id)
			}
			seen[key] = id
		}
		switch seat.Liveness {
		case Live, Stale, Dead:
		default:
			return fmt.Errorf("seat %s has invalid liveness %q", id, seat.Liveness)
		}
	}
	return nil
}

// Strict reports whether write-time seat enforcement is active for a loaded
// registry. Enforcement activates once a workspace has at least one
// registered seat: the first `bd seat register` (or the operator's
// `bd seat backfill` run) turns the workspace strict, and every later write
// resolves names against the registry. A workspace that has never registered
// a seat keeps legacy free-text identities so a fresh clone stays usable;
// there is no third mode and no scoring path.
func Strict(r Registry) bool { return len(r) > 0 }

// Resolve performs exact lookup only. It never creates a seat implicitly.
func (r Registry) Resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	for id, seat := range r {
		if strings.EqualFold(name, id) {
			return id, nil
		}
		for _, alias := range seat.Aliases {
			if strings.EqualFold(name, alias) {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("unknown seat %q: register a seat with 'bd seat register' or add an explicit alias with 'bd seat rename'", name)
}

func (r Registry) Display(id string) string {
	if seat, ok := r[id]; ok && len(seat.Aliases) > 0 {
		return seat.Aliases[0]
	}
	return id
}

func Load(dir string) (Registry, error) {
	data, err := os.ReadFile(filepath.Join(dir, File)) //nolint:gosec // workspace-selected registry path
	if os.IsNotExist(err) {
		return Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read seats: %w", err)
	}
	r := Registry{}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse seats: %w", err)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Save uses an atomic replacement and follows the canonical registry symlink.
func Save(dir string, r Registry) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, File)
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".seats-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "seats: cleanup: %v\n", err)
		}
	}()
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Register rebinds an existing ID on restart without changing its aliases.
func (r Registry) Register(id string, aliases []string, endpoint Endpoint, now time.Time) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if endpoint.Session == "" || endpoint.PaneID == "" {
		return fmt.Errorf("registration requires an exact endpoint session and pane id")
	}
	old, exists := r[id]
	if exists && len(aliases) > 0 {
		return fmt.Errorf("seat %s already exists: re-register without --alias; use rename to change aliases", id)
	}
	if !exists {
		if len(aliases) == 0 {
			aliases = []string{id}
		}
		old.Aliases = aliases
	}
	old.Endpoint, old.Session = endpoint, endpoint.AgentSessionID
	old.Liveness, old.UpdatedAt, old.LastSeenAt, old.MissingSince = Live, now.UTC(), now.UTC(), nil
	previous, had := r[id]
	r[id] = old
	if err := r.Validate(); err != nil {
		if had {
			r[id] = previous
		} else {
			delete(r, id)
		}
		return err
	}
	return nil
}

// Rename edits one registry row; it never visits issue or outbox records.
func (r Registry) Rename(id string, aliases []string, now time.Time) error {
	old, ok := r[id]
	if !ok {
		return fmt.Errorf("unknown seat id %q", id)
	}
	if len(aliases) == 0 {
		return fmt.Errorf("rename requires at least one alias (first is the display name)")
	}
	next := old
	next.Aliases, next.UpdatedAt = aliases, now.UTC()
	r[id] = next
	if err := r.Validate(); err != nil {
		r[id] = old
		return err
	}
	return nil
}

type contextKey struct{}
type contextErrorKey struct{}
type directoryKey struct{}

func WithDirectory(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, directoryKey{}, dir)
}
func Directory(ctx context.Context) string { dir, _ := ctx.Value(directoryKey{}).(string); return dir }
func WithRegistryError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, contextErrorKey{}, err)
}
func ContextError(ctx context.Context) error {
	err, _ := ctx.Value(contextErrorKey{}).(error)
	return err
}

func WithRegistry(ctx context.Context, r Registry) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}
func FromContext(ctx context.Context) (Registry, bool) {
	if ctx == nil {
		return nil, false
	}
	r, ok := ctx.Value(contextKey{}).(Registry)
	return r, ok
}

// ResolveContext allows storage users to opt in to a workspace registry; the
// CLI always installs one, including an empty registry, before data commands.
// Resolution is exact and active only while the registry is strict
// (at least one registered seat); legacy workspaces pass names through.
func ResolveContext(ctx context.Context, name string) (string, error) {
	if err := ContextError(ctx); err != nil {
		return "", err
	}
	if r, ok := FromContext(ctx); ok && Strict(r) {
		return r.Resolve(name)
	}
	return name, nil
}
