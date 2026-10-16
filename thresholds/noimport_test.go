package thresholds_test

import (
	"os/exec"
	"strings"
	"testing"
)

// The contract module, tests included, must never depend on the engine or
// the backend: they import it, not the other way round.
func TestNoEngineImport(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go tool not found: %v", err)
	}
	cmd := exec.Command(goTool, "list", "-deps", "-test", "./...")
	cmd.Dir = ".." // the module root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	pkgs := strings.Fields(string(out))
	if len(pkgs) < 10 {
		t.Fatalf("go list returned %d packages; the check is not looking at the module", len(pkgs))
	}
	for _, p := range pkgs {
		if strings.Contains(p, "volt-test/engine") || strings.Contains(p, "backend-portal") {
			t.Errorf("the contract module depends on %s", p)
		}
	}
}
