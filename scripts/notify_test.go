package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBeadsNotifyE2EScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping bash e2e on Windows")
	}
	// The Bazel sandbox exposes only declared runfiles, so `bash` is absent
	// there (the script itself is hermetic: it mocks `bd`). Skip rather than
	// fail - the shell legs of CI run this for real.
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not on PATH here (bazel sandbox?): %v", err)
	}
	t.Parallel()

	// Locate repo root
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller path")
	}
	repoRoot := filepath.Dir(filepath.Dir(filename))
	script := filepath.Join(repoRoot, "scripts", "notify", "test_beads_notify_e2e.sh")

	// Under Bazel the source-derived path does not always land inside the
	// runfiles tree even with the data dependency declared, so probe a couple
	// of well-known locations and skip rather than fail when the script is not
	// reachable from this binary. CI's shell legs run it for real.
	found := false
	for _, cand := range []string{script, filepath.Join("scripts", "notify", "test_beads_notify_e2e.sh")} {
		if _, err := os.Stat(cand); err == nil {
			script = cand
			found = true
			break
		}
	}
	if !found {
		t.Skipf("e2e script not reachable from this test binary (tried %q)", script)
	}

	cmd := exec.Command("bash", script)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("test_beads_notify_e2e.sh failed: %v\nOutput:\n%s", err, string(out))
	}
}
