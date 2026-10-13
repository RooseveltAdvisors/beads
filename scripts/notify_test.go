package scripts_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBeadsNotifyE2EScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping bash e2e on Windows")
	}
	t.Parallel()

	// Locate repo root
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	repoRoot := filepath.Dir(filepath.Dir(filename))
	script := filepath.Join(repoRoot, "scripts", "notify", "test_beads_notify_e2e.sh")

	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("test_beads_notify_e2e.sh failed: %v\nOutput:\n%s", err, string(out))
	}
}
