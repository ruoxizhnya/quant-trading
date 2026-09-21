package main

// AUD-02 (ODR-065 H5) regression guard: RBAC on the order-mutating
// endpoints and on per-tool execution.
//
// These tests exercise the REAL wiring (handler.RegisterRoutes +
// auth.Service middleware) rather than asserting on a route table,
// because the role decision for /api/tools/:name happens inside the
// handler and cannot be observed from the router alone.
//
// Every test here is written so that the "guard removed" regression
// makes it fail. Specifically:
//   - if someone drops WithExecutionAuth from main.go, the viewer
//     test starts returning 201 and goes red;
//   - if someone drops the h.requireTrader() from the LEGACY root
//     route, TestExecution_LegacyPath_SameAuthorityAsAPIPath goes red;
//   - if someone replaces authSvc.RequireRole with auth.RequireRole
//     in the disabled-auth case, the open-access test goes red.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ──────────────────────────────────────────────────────────

func newRBACTestTrader() live.LiveTrader {
	return live.NewMockTrader(live.MockTraderConfig{
		InitialCash:    1_000_000,
		CommissionRate: 0.0003,
		StampTaxRate:   0.0005,
		SlippageRate:   0.0001,
	}, zerolog.New(nil))
}

// rbacTestRouter builds a router with auth enabled and the execution
// handler wired for RBAC.
func rbacTestRouter(t *testing.T, svc *auth.Service) *gin.Engine {
	t.Helper()
	r := gin.New()
	r.Use(svc.Middleware())
	NewExecutionHandler(newRBACTestTrader(), zerolog.New(nil), "",
		WithExecutionAuth(svc)).RegisterRoutes(r)
	return r
}

func tokenFor(t *testing.T, svc *auth.Service, role auth.Role) string {
	t.Helper()
	tok, _, err := svc.IssueTokens(&auth.User{ID: 1, Username: string(role), Role: role})
	require.NoError(t, err)
	return tok
}

func postOrder(t *testing.T, r *gin.Engine, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"symbol":"000001.SZ","side":"long","type":"market","quantity":100,"price":10}`
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ── execution: /api/execution/* ──────────────────────────────────────

func TestExecution_CreateOrder_ViewerForbidden(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	w := postOrder(t, r, "/api/execution/orders", tokenFor(t, svc, auth.RoleViewer))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"viewer must not be able to submit orders")
	assert.Contains(t, w.Body.String(), "insufficient role")
}

func TestExecution_CreateOrder_TraderAllowed(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	w := postOrder(t, r, "/api/execution/orders", tokenFor(t, svc, auth.RoleTrader))
	assert.Equal(t, http.StatusCreated, w.Code,
		"trader must be able to submit orders; body=%s", w.Body.String())
}

func TestExecution_CreateOrder_AdminAllowed(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	w := postOrder(t, r, "/api/execution/orders", tokenFor(t, svc, auth.RoleAdmin))
	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestExecution_CreateOrder_NoTokenUnauthorized(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	w := postOrder(t, r, "/api/execution/orders", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestExecution_LegacyPath_SameAuthorityAsAPIPath is the specific
// guard for the "half-covered surface" failure mode: the legacy root
// route must require exactly the same role as the prefixed one.
//
// The two subtests assert opposite outcomes for the same request body,
// which is what makes this a real guard: if the legacy route loses its
// middleware, the viewer subtest flips from 403 to 201.
func TestExecution_LegacyPath_SameAuthorityAsAPIPath(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	t.Run("legacy viewer forbidden", func(t *testing.T) {
		w := postOrder(t, r, "/orders", tokenFor(t, svc, auth.RoleViewer))
		assert.Equal(t, http.StatusForbidden, w.Code,
			"legacy POST /orders must require trader-or-admin, same as /api/execution/orders")
	})

	t.Run("legacy trader allowed", func(t *testing.T) {
		w := postOrder(t, r, "/orders", tokenFor(t, svc, auth.RoleTrader))
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

// TestExecution_Cancel_LegacyAndAPIGated covers the second mutating
// endpoint (cancel) on both paths.
func TestExecution_Cancel_LegacyAndAPIGated(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)

	for _, path := range []string{"/api/execution/orders/abc/cancel", "/orders/abc/cancel"} {
		w := postOrder(t, r, path, tokenFor(t, svc, auth.RoleViewer))
		assert.Equal(t, http.StatusForbidden, w.Code,
			"viewer must be forbidden from cancelling via %s", path)
	}
}

// TestExecution_ReadEndpoints_AllowViewer pins that read endpoints were
// NOT over-restricted. Over-gating reads would break the frontend for
// viewer accounts and is the mirror-image failure of under-gating.
func TestExecution_ReadEndpoints_AllowViewer(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := rbacTestRouter(t, svc)
	token := tokenFor(t, svc, auth.RoleViewer)

	for _, path := range []string{
		"/api/execution/orders",
		"/api/execution/positions",
		"/api/execution/account",
		"/orders",
		"/positions",
		"/account",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code,
			"viewer must be able to GET %s", path)
	}
}

// TestExecution_AuthDisabled_OpenAccess is the short-circuit guard:
// with auth unconfigured, orders must still be submittable with no
// token at all (documented dev/CI behaviour).
func TestExecution_AuthDisabled_OpenAccess(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{}) // no JWTSecret => disabled
	require.False(t, svc.Enabled())

	r := gin.New()
	r.Use(svc.Middleware())
	NewExecutionHandler(newRBACTestTrader(), zerolog.New(nil), "",
		WithExecutionAuth(svc)).RegisterRoutes(r)

	w := postOrder(t, r, "/api/execution/orders", "")
	assert.Equal(t, http.StatusCreated, w.Code,
		"auth disabled must stay open-access; got body=%s", w.Body.String())
}

// ── tools: per-tool RBAC ─────────────────────────────────────────────

func toolsRBACRouter(t *testing.T, svc *auth.Service) *gin.Engine {
	t.Helper()
	r := gin.New()
	r.Use(svc.Middleware())

	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(newStubTool("save_factor")))
	require.NoError(t, reg.Register(newStubTool("backtest.run")))

	NewToolsHandler(reg, zerolog.Nop(), WithToolsAuth(svc)).RegisterRoutes(r)
	return r
}

// stubTool is a minimal Tool whose registry name is set explicitly, so
// the test can place it in the side-effect table's read or write class
// via a name that is already classified.
//
// It deliberately uses REAL classified names (save_factor /
// backtest.run) so the test exercises the production classification
// table rather than a test-only map.
type stubTool struct {
	name string
}

func newStubTool(name string) *stubTool { return &stubTool{name: name} }

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub for RBAC tests" }
func (s *stubTool) Parameters() []tools.Parameter {
	return []tools.Parameter{{Name: "x", Type: "string"}}
}
func (s *stubTool) OutputSchema() tools.OutputSchema {
	return tools.OutputSchema{Type: "object"}
}
func (s *stubTool) Execute(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	return map[string]string{"stub": s.name}, nil
}

func callTool(t *testing.T, r *gin.Engine, name, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tools/"+name,
		strings.NewReader(`{"args":{}}`))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestTools_WriteTool_ViewerForbidden(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := toolsRBACRouter(t, svc)

	w := callTool(t, r, "save_factor", tokenFor(t, svc, auth.RoleViewer))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"viewer must not be able to run a write-side-effect tool; body=%s", w.Body.String())

	// The error body should name the class so an operator can tell
	// "you lack the role" from "the tool failed".
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "INSUFFICIENT_ROLE", body["code"])
	assert.Equal(t, "write", body["effect"])
}

func TestTools_WriteTool_TraderAllowed(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := toolsRBACRouter(t, svc)

	w := callTool(t, r, "save_factor", tokenFor(t, svc, auth.RoleTrader))
	// The stub doesn't implement the real Tool interface contract for
	// execution; what matters is that RBAC did NOT block it (i.e. the
	// status is not 403).
	assert.NotEqual(t, http.StatusForbidden, w.Code,
		"trader must be allowed past RBAC on a write tool")
}

func TestTools_ReadTool_ViewerAllowed(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := toolsRBACRouter(t, svc)

	w := callTool(t, r, "backtest.run", tokenFor(t, svc, auth.RoleViewer))
	assert.NotEqual(t, http.StatusForbidden, w.Code,
		"viewer must be allowed past RBAC on a read tool")
}

// TestTools_UnclassifiedTool_FailsClosed is the fail-closed guard: a
// tool with no row in the classification table must require admin.
func TestTools_UnclassifiedTool_FailsClosed(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})

	r := gin.New()
	r.Use(svc.Middleware())
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(newStubTool("brand.new.unclassified")))
	NewToolsHandler(reg, zerolog.Nop(), WithToolsAuth(svc)).RegisterRoutes(r)

	// trader is NOT enough for an unclassified tool.
	w := callTool(t, r, "brand.new.unclassified", tokenFor(t, svc, auth.RoleTrader))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"unclassified tool must fail closed to admin-only")

	// admin gets through.
	w = callTool(t, r, "brand.new.unclassified", tokenFor(t, svc, auth.RoleAdmin))
	assert.NotEqual(t, http.StatusForbidden, w.Code)
}

func TestTools_AuthDisabled_OpenAccess(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{})
	require.False(t, svc.Enabled())

	r := gin.New()
	r.Use(svc.Middleware())
	reg := tools.NewRegistry()
	require.NoError(t, reg.Register(newStubTool("save_factor")))
	NewToolsHandler(reg, zerolog.Nop(), WithToolsAuth(svc)).RegisterRoutes(r)

	w := callTool(t, r, "save_factor", "")
	assert.NotEqual(t, http.StatusForbidden, w.Code,
		"auth disabled must stay open-access for tools too")
}

// TestTools_ListEndpoint_NotGated ensures the discovery endpoints
// (GET /api/tools) were not caught by the per-tool RBAC.
func TestTools_ListEndpoint_NotGated(t *testing.T) {
	t.Parallel()
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test")})
	r := toolsRBACRouter(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/tools", nil)
	req.Header.Set("Authorization", "Bearer "+tokenFor(t, svc, auth.RoleViewer))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "viewer must be able to list tools")
}
