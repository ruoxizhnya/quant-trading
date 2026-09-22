package api

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain sets gin to test mode once for the entire package.
//
// AUD-28 (ODR-065): this replaces the `func init() { gin.SetMode(...) }`
// that used to live in versioning_test.go. init() was already race-safe
// (it runs once, before any test goroutine), but it is not greppable: a
// reader who finds `gin.SetMode` in a test file cannot tell whether the
// package centralises the call or calls it per test. TestMain is the
// repo-wide convention for this (AUD-12: cmd/analysis, pkg/auth), so the
// invariant is now one `grep -rn "func TestMain" <pkg>/` away.
//
// Individual tests must NOT call gin.SetMode() — see the longer note in
// internal/httpserver/main_test.go for the race it creates.
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}
