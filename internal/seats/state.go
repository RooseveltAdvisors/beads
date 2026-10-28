package seats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StateEnv names the environment variable that carries the path of the
// calling agent's durable state file. An agent (or its spawner) points it at
// storage the agent keeps across restarts; the seat register command and the
// runtime rebind read the uuid and rebind token from it. Identity never
// comes from a pane, tab, or session.
const StateEnv = "BEADS_AGENT_STATE"

// AgentState is the agent-owned half of a minted seat identity. Mint writes
// it once; every later start reads it to re-register the endpoint under the
// same uuid. The rebind token is a bearer secret: the file is written
// owner-only and the registry stores only its hash.
type AgentState struct {
	AgentUUID   string `json:"agent_uuid"`
	Alias       string `json:"alias,omitempty"`
	RebindToken string `json:"rebind_token"`
	MintedAt    string `json:"minted_at"`
	UpdatedAt   string `json:"updated_at"`
}

// WriteState persists the agent durable state atomically with owner-only
// permissions. Losing this file strands the seat: without the token no
// process can re-register the uuid, so write failure after a mint must
// surface loudly.
func WriteState(path string, st AgentState) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("agent state: empty path")
	}
	if strings.TrimSpace(st.AgentUUID) == "" {
		return fmt.Errorf("agent state: empty uuid")
	}
	if strings.TrimSpace(st.RebindToken) == "" {
		return fmt.Errorf("agent state: empty rebind token")
	}
	if st.MintedAt == "" {
		st.MintedAt = time.Now().UTC().Format(time.RFC3339)
	}
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".agent-state-*")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "agent state: cleanup: %v\n", err)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// ReadState loads the agent durable state. A missing file returns an error
// wrapping os.ErrNotExist so callers can tell "never minted" apart from a
// corrupt state, which must refuse rather than silently re-mint.
func ReadState(path string) (AgentState, error) {
	var st AgentState
	if strings.TrimSpace(path) == "" {
		return st, fmt.Errorf("agent state: empty path")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the state path is the caller's own durable state location
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("parse agent state %s: %w", path, err)
	}
	if strings.TrimSpace(st.AgentUUID) == "" || strings.TrimSpace(st.RebindToken) == "" {
		return st, fmt.Errorf("agent state %s is incomplete (uuid and rebind token required)", path)
	}
	return st, nil
}
