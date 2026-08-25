package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTheProductionBinaryCannotContainTheFake guards the one property that
// makes an offline demo safe to have at all: the stand-in GitHub, and the demo
// that starts it, are reachable from cmd/demo alone. Nothing in the deployed
// binary links them, so no configuration, flag or environment variable can turn
// a real deployment into one that fakes its own sign-in.
//
// admin-app/Dockerfile builds this package and only this package, so the
// dependency list below is exactly what ships.
func TestTheProductionBinaryCannotContainTheFake(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/internal/ghfake") || strings.HasSuffix(dep, "/cmd/demo") {
			t.Errorf("the production binary depends on %s — the demo must stay unreachable from it", dep)
		}
	}
}
