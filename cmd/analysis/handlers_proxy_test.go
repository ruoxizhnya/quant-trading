package main

// handlers_proxy_test.go — TASKS.md P5-3 (citation coordinates at the gateway)
//
// The analysis gateway is the only door the SPA has into L0 (ADR-022 §3, one
// direction only). GET /api/factors/:factor_name must therefore (a) add the
// /api prefix the SPA uses onto the prefix-less L0 route, (b) forward the
// symbol/date selectors, and (c) pass the L0 status/body through untouched —
// a hash-only citation is a first-class answer, not something to rewrite.

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newProxyTestRouter wires registerProxyRoutes against a fake L0 data service
// and returns the router plus every request the upstream received.
func newProxyTestRouter(t *testing.T, upstream http.HandlerFunc) (*gin.Engine, *[]*http.Request) {
	t.Helper()

	received := &[]*http.Request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*received = append(*received, r)
		upstream(w, r)
	}))
	t.Cleanup(server.Close)

	v := viper.New()
	v.Set("data_service.url", server.URL)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerProxyRoutes(router, server.Client(), v, zerolog.Nop())
	return router, received
}

func jsonUpstream(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// TestFactorCitationProxy_ForwardsSelectorsToPrefixlessL0Route pins the path
// shape: the SPA asks for /api/factors/<name>, L0 serves /factors/<name>.
func TestFactorCitationProxy_ForwardsSelectorsToPrefixlessL0Route(t *testing.T) {
	router, received := newProxyTestRouter(t, jsonUpstream(http.StatusOK, `{
		"id": 1,
		"symbol": "000001.SZ",
		"factor_name": "momentum",
		"raw_value": 1.25,
		"citation": [{"content_hash": "aa", "source": "tushare", "dataset": "daily"}]
	}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/factors/momentum?symbol=000001.SZ&date=20260105", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.Len(t, *received, 1)
	assert.Equal(t, "/factors/momentum", (*received)[0].URL.Path)
	assert.Equal(t, "000001.SZ", (*received)[0].URL.Query().Get("symbol"))
	assert.Equal(t, "20260105", (*received)[0].URL.Query().Get("date"))
	assert.Contains(t, w.Body.String(), `"source":"tushare"`)
}

// TestFactorCitationProxy_OmitsEmptyQuery verifies selectors are forwarded
// only when present, so L0 keeps ownership of the required-param errors.
func TestFactorCitationProxy_OmitsEmptyQuery(t *testing.T) {
	router, received := newProxyTestRouter(t, jsonUpstream(http.StatusBadRequest, `{"error":"symbol query param is required"}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/factors/momentum", nil)
	router.ServeHTTP(w, req)

	require.Len(t, *received, 1)
	assert.Equal(t, "/factors/momentum", (*received)[0].URL.Path)
	assert.Empty(t, (*received)[0].URL.RawQuery)
	// L0's own validation answer travels through unchanged.
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "symbol query param is required")
}

// TestFactorCitationProxy_PassesNotFoundThrough: a row-less lookup is a
// first-class 404 answer, and the gateway must not launder it into a 5xx.
func TestFactorCitationProxy_PassesNotFoundThrough(t *testing.T) {
	router, _ := newProxyTestRouter(t, jsonUpstream(http.StatusNotFound, `{"error":"factor cache entry not found"}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/factors/momentum?symbol=000001.SZ&date=20260105", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "factor cache entry not found")
}

// TestFactorCitationProxy_KeepsHashOnlyTuple: when the source response was
// never archived the tuple carries only content_hash — the absence of the
// other four fields is the signal, so nothing may be padded in transit.
func TestFactorCitationProxy_KeepsHashOnlyTuple(t *testing.T) {
	router, _ := newProxyTestRouter(t, jsonUpstream(http.StatusOK, `{
		"symbol": "000001.SZ",
		"factor_name": "momentum",
		"citation": [{"content_hash": "bb"}]
	}`))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/factors/momentum?symbol=000001.SZ&date=20260105", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"content_hash":"bb"`)
	assert.NotContains(t, w.Body.String(), `"source"`)
	assert.NotContains(t, w.Body.String(), `"dataset"`)
}