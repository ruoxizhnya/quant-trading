package httpserver

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// ConfigKeyGinMode is the viper key that selects gin's run mode.
//
// AUD-29 (ODR-065): gin's run mode used to be derived from the logging
// config — `logging.format == "json"` in cmd/analysis, `logging.level !=
// "debug"` in cmd/data and cmd/strategy. Two different heuristics for one
// decision, so the same config produced different gin modes per service;
// and both key off a setting that has nothing to do with how chatty gin
// is (log format / log level vs. framework debug output). It was also
// written at router-build time, i.e. a process-wide global mutated from
// inside a function that tests call.
//
// Now the mode comes from this one explicit key, applied exactly once
// during startup (ApplyGinMode) before any router is built.
const ConfigKeyGinMode = "server.gin_mode"

// Gin run modes accepted in ConfigKeyGinMode.
const (
	// GinModeDebug keeps gin's route-registration spew and its
	// "[GIN-debug]" banner. Local development only.
	GinModeDebug = "debug"
	// GinModeRelease is gin's production mode: no debug output.
	GinModeRelease = "release"
	// GinModeTest silences gin and suppresses its warnings. Used by tests.
	GinModeTest = "test"
)

// ApplyGinMode sets gin's global run mode from the raw server.gin_mode
// value. It returns the mode actually applied and whether the configured
// value was recognized.
//
// Call it exactly once, early in startup, before any router is built.
// gin keeps its mode in two package-level globals (ginMode / modeName)
// behind plain — not atomic — assignments, and RouterGroup.handle reads
// them on every route registration (handle → debugPrintRoute →
// IsDebugging). So this is a process-wide write that must not happen
// later than startup, nor from more than one place.
//
// Unset ("") and unrecognized values fall back to release — the fail-safe
// direction. gin's own default is debug, and a typo must never be the
// reason a deployed process starts printing its route table. The caller
// is expected to log a warning when recognized is false.
//
// This is the only production writer of gin's mode; the rest of the repo
// is kept honest by TestGinSetModeOnlyInSanctionedPlaces.
func ApplyGinMode(raw string) (applied string, recognized bool) {
	mode, recognized := ginModeFor(raw)
	// gin.SetMode panics on an unknown value, so the raw string is never
	// passed through — ginModeFor is the only thing that picks the mode.
	gin.SetMode(mode)
	return mode, recognized
}

// ginModeFor maps a raw server.gin_mode value to a gin mode name and
// reports whether the value was recognized.
//
// Pure on purpose: no globals are touched, so it can be exercised by
// parallel tests without racing the rest of the package (gin's own mode
// lives in unsynchronized package-level variables — see ApplyGinMode).
func ginModeFor(raw string) (mode string, recognized bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case GinModeDebug:
		return GinModeDebug, true
	case GinModeTest:
		return GinModeTest, true
	case GinModeRelease:
		return GinModeRelease, true
	case "":
		// Not configured: documented default.
		return GinModeRelease, true
	default:
		return GinModeRelease, false
	}
}
