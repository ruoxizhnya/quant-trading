// Package guard contains regression guards for the e2e test suite.
// It lives in a separate package so it is NOT skipped by the
// TestMain skip-guard in e2e/tests/integration_test.go.
package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationTestHasSkipGuard is a regression guard for S7-P0-8
// (ODR-043-6): e2e/tests/integration_test.go must define a TestMain
// that skips when Docker services are unreachable. Without it,
// `go test ./...` fails whenever Docker Compose isn't running.
func TestIntegrationTestHasSkipGuard(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "tests", "integration_test.go"))
	if err != nil {
		t.Fatalf("read integration_test.go: %v", err)
	}
	s := string(source)
	if !strings.Contains(s, "func TestMain(") {
		t.Error("e2e/tests/integration_test.go must define TestMain for skip-guard (S7-P0-8)")
	}
	if !strings.Contains(s, "servicesReachable") {
		t.Error("e2e/tests/integration_test.go must probe service reachability in TestMain (S7-P0-8)")
	}
}
