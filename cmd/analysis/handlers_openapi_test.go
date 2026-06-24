package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/docs"
)

// newOpenAPITestRouter builds a minimal gin router with only the
// OpenAPI routes registered. Keeping it isolated means the tests
// don't need a database, config, or any of the heavy main.go wiring.
func newOpenAPITestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerOpenAPIRoutes(router)
	return router
}

// TestServeOpenAPISpec_Returns200 verifies that GET /api/openapi.yaml
// returns HTTP 200 and a non-empty body.
func TestServeOpenAPISpec_Returns200(t *testing.T) {
	router := newOpenAPITestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "expected 200 OK")
	assert.NotEmpty(t, w.Body.Bytes(), "spec body should not be empty")
}

// TestServeOpenAPISpec_ContentType verifies the Content-Type header
// is set to application/yaml so browsers and tools recognise the
// response as YAML.
func TestServeOpenAPISpec_ContentType(t *testing.T) {
	router := newOpenAPITestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	router.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	assert.Contains(t, ct, "application/yaml",
		"Content-Type should be application/yaml, got %q", ct)
}

// TestServeOpenAPISpec_ContainsOpenAPIKeys verifies the served spec
// contains the expected top-level OpenAPI 3.0 keys and the info
// title, confirming the embedded content is the actual spec.
func TestServeOpenAPISpec_ContainsOpenAPIKeys(t *testing.T) {
	router := newOpenAPITestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()
	assert.Contains(t, body, "openapi: 3.0", "spec should declare OpenAPI 3.0")
	assert.Contains(t, body, "title: Quant Lab", "spec should contain the info title")
	assert.Contains(t, body, "paths:", "spec should contain a paths section")
	assert.Contains(t, body, "/api/backtest", "spec should document the backtest endpoint")
}

// TestServeSwaggerUI_ReturnsHTML verifies that GET /api/docs returns
// HTTP 200 and an HTML content type.
func TestServeSwaggerUI_ReturnsHTML(t *testing.T) {
	router := newOpenAPITestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "expected 200 OK")
	ct := w.Header().Get("Content-Type")
	assert.Contains(t, ct, "text/html",
		"Content-Type should be text/html, got %q", ct)
}

// TestServeSwaggerUI_ContainsSwaggerUIAssets verifies the Swagger UI
// HTML references the CDN-hosted swagger-ui-dist bundle and points
// at the /api/openapi.yaml spec URL.
func TestServeSwaggerUI_ContainsSwaggerUIAssets(t *testing.T) {
	router := newOpenAPITestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	router.ServeHTTP(w, req)

	body := w.Body.String()
	assert.True(t, strings.Contains(body, "swagger-ui-bundle.js"),
		"Swagger UI HTML should load swagger-ui-bundle.js")
	assert.True(t, strings.Contains(body, "/api/openapi.yaml"),
		"Swagger UI HTML should point at /api/openapi.yaml")
	assert.True(t, strings.Contains(body, "swagger-ui.css"),
		"Swagger UI HTML should load swagger-ui.css")
}

// TestEmbeddedSpecIsNonEmpty is a sanity check on the embedded
// variable itself — if the embed failed at compile time the test
// suite would not run, but this guards against an empty file
// slipping through.
func TestEmbeddedSpecIsNonEmpty(t *testing.T) {
	require.NotEmpty(t, docs.OpenAPISpec,
		"embedded OpenAPISpec should not be empty")
	assert.True(t, len(docs.OpenAPISpec) > 1000,
		"embedded spec should be substantial (>1KB), got %d bytes", len(docs.OpenAPISpec))
}
