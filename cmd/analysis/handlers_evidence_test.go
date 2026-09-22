package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// evidenceTestDSN mirrors pkg/storage's testStore helper: if the compose
// Postgres is not running the test skips rather than fails.
const evidenceTestDSN = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"

// newEvidenceTestRouter wires the Evidence handler onto a bare gin router.
// Returns the store too, so tests can seed ingest.raw rows directly.
func newEvidenceTestRouter(t *testing.T) (*gin.Engine, *storage.PostgresStore) {
	t.Helper()
	store, err := storage.NewPostgresStore(context.Background(), evidenceTestDSN)
	if err != nil {
		t.Skipf("skipping test: cannot connect to DB: %v", err)
	}
	router := gin.New()
	NewEvidenceHandler(store, zerolog.Nop()).RegisterRoutes(router)
	return router, store
}

// TestNewEvidenceHandler_PanicsOnNilStore verifies the fail-loud wiring
// contract shared by the pattern-B handlers.
func TestNewEvidenceHandler_PanicsOnNilStore(t *testing.T) {
	assert.Panics(t, func() {
		NewEvidenceHandler(nil, zerolog.Nop())
	})
}

// TestEvidenceHandler_UnknownHash_Returns404 verifies the "not ingested"
// signal — the citation cannot be resolved, so the API answers 404 rather
// than fabricating an empty record.
func TestEvidenceHandler_UnknownHash_Returns404(t *testing.T) {
	router, store := newEvidenceTestRouter(t)
	defer store.Close()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/evidence/"+strings.Repeat("0", 64), nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not ingested")
}

// TestEvidenceHandler_ReturnsArchivedRecord verifies the round trip a work
// face performs: hold a content_hash from a citation, ask the platform for
// the exact source response behind it, and get the archived payload back.
func TestEvidenceHandler_ReturnsArchivedRecord(t *testing.T) {
	router, store := newEvidenceTestRouter(t)
	defer store.Close()
	ctx := context.Background()

	const payload = `{"instrument":"600519.SH","period":"2024Q1","net_profit":240000000,"_probe":"evidence-handler-v1"}`
	hash, err := storage.ContentHashOf([]byte(payload))
	require.NoError(t, err)

	require.NoError(t, store.SaveRawIngest(ctx, &storage.RawIngest{
		ContentHash: hash,
		Source:      "unit-test",
		Dataset:     "fundamentals.income",
		Key:         "600519.SH/2024Q1",
		Payload:     json.RawMessage(payload),
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/evidence/"+hash, nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var got storage.RawIngest
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, hash, got.ContentHash)
	assert.Equal(t, "unit-test", got.Source)
	assert.Equal(t, "fundamentals.income", got.Dataset)
	assert.Equal(t, "600519.SH/2024Q1", got.Key)
	assert.JSONEq(t, payload, string(got.Payload))
}
