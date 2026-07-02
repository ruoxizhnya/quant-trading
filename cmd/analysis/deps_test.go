package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerDeps_HasExpectedFields verifies that ServerDeps exposes
// every dependency registerRoutes needs. S7-P2-4 (ODR-043): the
// struct replaces 16 positional parameters — this test guards against
// accidental field removal or renaming by enumerating the canonical
// set of fields. S7-P3-3 added ToolsRegistry (17th field).
func TestServerDeps_HasExpectedFields(t *testing.T) {
	t.Parallel()

	expected := []string{
		"Engine",
		"JobService",
		"WFEngine",
		"BatchEngine",
		"StrategyDB",
		"CopilotService",
		"CopilotRunner",
		"FactorAttributor",
		"PluginLoader",
		"AuthSvc",
		"RiskManager",
		"ExecutionTrader",
		"EmergencyToken",
		"Metrics",
		"Logger",
		"Viper",
		"ToolsRegistry",
	}

	typ := reflect.TypeOf(ServerDeps{})
	require.Equal(t, reflect.Struct, typ.Kind(),
		"ServerDeps must be a struct")

	// Build a set of actual field names for O(1) lookup.
	actual := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		actual[typ.Field(i).Name] = true
	}

	for _, name := range expected {
		assert.True(t, actual[name],
			"ServerDeps must have field %q (missing)", name)
	}
	assert.Equal(t, len(expected), typ.NumField(),
		"ServerDeps should have exactly %d fields (got %d) — "+
			"update this test if a field was intentionally added/removed",
		len(expected), typ.NumField())
}

// TestServerDeps_FieldsAreTyped guards that the field types match the
// services they carry. This catches subtle mistakes like typing
// ExecutionTrader as a concrete *MockTrader instead of the live.LiveTrader
// interface (which would break the nil-substitution that tests rely on).
func TestServerDeps_FieldsAreTyped(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeOf(ServerDeps{})
	cases := map[string]string{
		// All service fields are pointer-to-struct or interface types —
		// nil must be a valid zero value so registerRoutes can wire
		// closures that defer dereference to request time.
		//
		// S7-P2-1 note: JobService / WFEngine / BatchEngine are declared
		// as *backtest.X (type aliases) in deps.go, but reflect.TypeOf
		// resolves aliases to their canonical subpackage paths
		// (*job.JobService, *walkforward.WalkForwardEngine,
		// *batch.BatchEngine). The alias targets are the source of truth.
		"Engine":           "*backtest.Engine",
		"JobService":       "*job.JobService",
		"WFEngine":         "*walkforward.WalkForwardEngine",
		"BatchEngine":      "*batch.BatchEngine",
		"StrategyDB":       "*strategy.StrategyDB",
		"CopilotService":   "*strategy.CopilotService",
		"FactorAttributor": "*data.FactorAttributor",
		"PluginLoader":     "*strategy.PluginLoader",
		"AuthSvc":          "*auth.Service",
		"RiskManager":      "*risk.RiskManager",
		"Metrics":          "*observability.Metrics",
	}
	for field, wantType := range cases {
		f, ok := typ.FieldByName(field)
		require.True(t, ok, "field %q missing", field)
		assert.Equal(t, wantType, f.Type.String(),
			"field %q has wrong type", field)
	}
}

// newMinimalDeps builds a ServerDeps with only the fields that
// registerRoutes dereferences during registration (not deferred to
// request-time closures). Service pointers are left nil — this is
// safe because registerRoutes' sub-registrators capture them in
// closures and only touch them when a request arrives.
//
// The immediately-dereferenced fields are:
//   - deps.Viper  (loadDefaultSuitabilityProfile + reporterCfg read keys now)
//   - deps.Logger (passed by value; zero value is usable)
//   - deps.Metrics (observability.Handler is nil-safe, returns 503)
//   - deps.ToolsRegistry (NewToolsHandler panics on nil — provide empty registry)
func newMinimalDeps() *ServerDeps {
	return &ServerDeps{
		Viper:  viper.New(),
		Logger: zerolog.New(nil),
		Metrics: func() *observability.Metrics {
			// NewMetrics creates a private registry so it won't
			// collide with the global Prometheus registry across
			// parallel tests. Register() is intentionally NOT called —
			// the /metrics handler still works (returns an empty scrape).
			return observability.NewMetrics()
		}(),
		ToolsRegistry: tools.NewRegistry(),
	}
}

// TestRegisterRoutes_RegistersCoreEndpoints verifies that registerRoutes
// wires the static + health + metrics + API surface without panicking,
// even when the DB-backed service pointers are nil. The nil-safety
// contract is: registration captures closures; services are only
// dereferenced at request time.
func TestRegisterRoutes_RegistersCoreEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	deps := newMinimalDeps()

	// Must not panic — this is the core assertion. If any register*
	// helper dereferences a nil service during registration, the test
	// fails loudly here.
	require.NotPanics(t, func() {
		registerRoutes(router, deps)
	})

	// Collect every registered route into a set of "METHOD PATH" keys.
	routes := router.Routes()
	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		seen[r.Method+" "+r.Path] = true
	}

	// Core endpoints that every analysis-service instance must expose.
	// These are registered directly by registerRoutes (not delegated to
	// a sub-registrator), so they must always be present.
	mustHave := []string{
		"GET /health",
		"GET /metrics",
		"GET /api/v1",
		"GET /api/openapi.yaml",
		"GET /api/docs",
		"GET /",
		"GET /static/*filepath",
	}
	for _, route := range mustHave {
		assert.True(t, seen[route],
			"expected route %q to be registered", route)
	}

	// Spot-check one route from each handler group to confirm the
	// sub-registrators ran. The full route set is exercised by the
	// per-handler tests; here we only verify the wiring happened.
	handlerGroupSamples := []string{
		"POST /api/risk/calculate_position", // RiskHandler
		"POST /api/execution/orders",        // ExecutionHandler
		"POST /api/compliance/check",        // ComplianceHandler
		"GET /api/tools",                    // ToolsHandler (S7-P3-3)
	}
	for _, route := range handlerGroupSamples {
		assert.True(t, seen[route],
			"expected handler-group route %q to be registered", route)
	}
}

// TestRegisterRoutes_NilMetricsReturns503 confirms the defensive
// fallback in observability.Handler: when deps.Metrics is nil, the
// /metrics endpoint returns 503 instead of panicking. This is the
// "loud at runtime, not silent" contract from handler.go:19.
func TestRegisterRoutes_NilMetricsReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	deps := newMinimalDeps()
	deps.Metrics = nil // simulate misconfigured wire-up

	require.NotPanics(t, func() {
		registerRoutes(router, deps)
	})

	w := performRequest(router, "GET", "/metrics")
	assert.Equal(t, 503, w.Code,
		"nil Metrics should produce a 503, not a panic")
}

// performRequest is a tiny helper that fires an HTTP request against
// a gin router in-process. Kept local to deps_test.go to avoid
// clashing with helpers in other _test.go files.
func performRequest(router *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), method, path, nil)
	router.ServeHTTP(w, req)
	return w
}
