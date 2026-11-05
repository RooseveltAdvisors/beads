package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/notify"
	"github.com/steveyegge/beads/internal/seats"
)

// bindSeatIdentity runs against the final selected workspace, before writes.
func bindSeatIdentity(cmd *cobra.Command, beadsDir string, write bool) error {
	r, err := seats.Load(filepath.Join(beadsDir, notify.DirName))
	if err != nil {
		return err
	}
	rootCtx = seats.WithDirectory(seats.WithRegistry(rootCtx, r), filepath.Join(beadsDir, notify.DirName))
	// The registry commands are the bootstrap mechanism itself: registering,
	// renaming, unbinding and backfilling must work for an operator whose own
	// seat is not registered yet, so they never resolve an actor or a flag.
	// The notify commands only deliver/ack rows and stamp no bead identity;
	// their explicit --seat names still resolve exactly at the outbox.
	if cmd != nil && cmd.Parent() != nil {
		switch cmd.Parent().Name() {
		case seatCmd.Name(), notifyCmd.Name():
			return nil
		}
	}
	// A workspace with no registered seats keeps legacy free-text identities;
	// the first registered seat turns this workspace strict (seats.Strict).
	if !seats.Strict(r) {
		return nil
	}
	for _, key := range []string{"assignee", "owner", "if-assignee"} {
		flag := cmd.Flags().Lookup(key)
		if flag == nil || flag.Value.String() == "" {
			continue
		}
		id, err := r.Resolve(flag.Value.String())
		if err != nil {
			return err
		}
		if write && key == "assignee" && id != "" && !seats.IsUUID(id) && seats.MachineActor(id) == "" {
			return fmt.Errorf("assignee seat %q resolves to non-UUID identity %q: agent seat must be minted with a UUID (bd seat register --alias %s)", flag.Value.String(), id, flag.Value.String())
		}
		_ = flag.Value.Set("")
		if err := flag.Value.Set(id); err != nil {
			return err
		}
	}
	if !write {
		return nil
	}
	name := actor
	if cmd.Name() == "comment" || (cmd.Parent() != nil && cmd.Parent().Name() == "comments") {
		name = getCommentActor()
	}
	id, err := r.Resolve(name)
	if err != nil {
		return fmt.Errorf("actor: %w", err)
	}
	if id == "" {
		return fmt.Errorf("write requires a registered actor seat")
	}
	if !seats.IsUUID(id) && seats.MachineActor(id) == "" {
		return fmt.Errorf("actor seat %q has non-UUID identity %q: agent seat must be minted with a UUID (bd seat register --alias %s)", name, id, name)
	}
	actor = id
	return nil
}

func seatDisplay(id string) string {
	if r, ok := seats.FromContext(rootCtx); ok {
		disp := r.Display(id)
		if disp != id && seats.IsUUID(id) {
			return fmt.Sprintf("%s (%s)", id, disp)
		}
		return disp
	}
	return id
}

func seatDisplayShort(id string) string {
	if r, ok := seats.FromContext(rootCtx); ok {
		disp := r.Display(id)
		if disp != id && seats.IsUUID(id) {
			return fmt.Sprintf("%s [%s]", disp, id[:8])
		}
		if seats.IsUUID(id) && len(id) >= 8 {
			return id[:8]
		}
		return disp
	}
	if seats.IsUUID(id) && len(id) >= 8 {
		return id[:8]
	}
	return id
}

// projectSeatJSON creates detached output. Stored fields and routing continue
// to use IDs, even when a display alias has changed since the row was written.
func projectSeatJSON(v any) (any, error) {
	r, ok := seats.FromContext(rootCtx)
	if !ok || len(r) == 0 {
		return v, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	var visit func(any)
	visit = func(node any) {
		switch item := node.(type) {
		case []any:
			for _, child := range item {
				visit(child)
			}
		case map[string]any:
			// Ensure assignee holds the canonical UUID identity, not a display alias
			// (captain directive 2026-11-04: remove human name noise from identity fields).
			if _, issue := item["title"]; issue {
				if id, _ := item["assignee_id"].(string); id != "" {
					item["assignee"] = id
					if disp := r.Display(id); disp != id {
						item["assignee_display"] = disp
					}
				}
			}
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(out)
	return out, nil
}

func commandSeatBound() bool {
	r, ok := seats.FromContext(rootCtx)
	return ok && seats.Strict(r)
}
