// Package crashlog appends bd crashes and errors to a durable log file under
// the XDG state directory: $XDG_STATE_HOME/beads/logs/bd.log (default
// ~/.local/state/beads/logs/bd.log). Entries contain timestamps, error
// messages, and panic stack traces only: no issue bodies, credentials, or
// health data. Logging is best-effort and never fails a command.
package crashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// Dir returns the directory holding bd's durable error log ("" if unknown).
func Dir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "beads", "logs")
}

// Path returns the full path of the log file ("" if the state dir is unknown).
func Path() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "bd.log")
}

// Recover logs a panic with its stack, then re-panics to preserve the
// original crash behavior. Defer it from main. Note: it fires on panics only;
// os.Exit paths skip defers and are logged separately via Error.
func Recover() {
	if r := recover(); r != nil {
		write("panic: %v\n%s", r, debug.Stack())
		panic(r)
	}
}

// Error appends one error line attributed to the command that failed.
func Error(cmd string, err error) {
	if err == nil {
		return
	}
	write("%s: error: %v", cmd, err)
}

// ponytail: 5MB cap with truncate-on-overflow instead of rotation; add real
// rotation if users ever hit the cap.
const maxLogSize = 5 << 20

func write(format string, args ...any) {
	path := Path()
	if path == "" {
		return
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		_ = os.Truncate(path, 0)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format(time.RFC3339)+" "+format+"\n", args...)
}
