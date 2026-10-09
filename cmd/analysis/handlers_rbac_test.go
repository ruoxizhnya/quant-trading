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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
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
