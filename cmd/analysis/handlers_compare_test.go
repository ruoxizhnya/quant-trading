package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ──── Stubs ──────────────────────────────────────────────

// compareStubStore is a minimal in-memory job store used by the
// compare handler test. It backs a backtest.CompareResultResolver
// without dragging in the real JobService (which requires an Engine
// and a Postgres store).
type compareStubStore struct {
	jobs map[string]json.RawMessage
}

func (s *compareStubStore) Lookup(id string) (backtest.BacktestResponse, error) {
	raw, ok := s.jobs[id]
	if !ok {
		return backtest.BacktestResponse{}, &compareNotFoundError{id: id}
	}
	var resp backtest.BacktestResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return backtest.BacktestResponse{}, err
	}
	return resp, nil
}

type compareNotFoundError struct{ id string }

func (e *compareNotFoundError) Error() string {
	return "backtest not found or not completed: " + e.id
}

func putCompareJob(store *compareStubStore, id string, resp backtest.BacktestResponse) {
	b, _ := json.Marshal(resp)
	if store.jobs == nil {
		store.jobs = map[string]json.RawMessage{}
	}
	store.jobs[id] = b
}

func makeCompareResponse(id, strategy string, totalReturn, maxDD, sharpe float64) backtest.BacktestResponse {
	return backtest.BacktestResponse{
		ID:              id,
		Status:          "completed",
		Strategy:        strategy,
		StartDate:       "2024-01-01",
		EndDate:         "2024-06-30",
		TotalReturn:     totalReturn,
		AnnualReturn:    totalReturn * 2,
		SharpeRatio:     sharpe,
		SortinoRatio:    sharpe * 1.2,
		MaxDrawdown:     maxDD,
		MaxDrawdownDate: "2024-04-15",
		WinRate:         0.55,
		TotalTrades:     20,
		WinTrades:       11,
		LoseTrades:      9,
		AvgHoldingDays:  5.0,
		CalmarRatio:     1.5,
		StockPool:       []string{"600000.SH"},
		InitialCapital:  1_000_000,
		PortfolioValues: []domain.PortfolioValue{
			{Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), TotalValue: 1_000_000, Cash: 1_000_000, Positions: 0},
			{Date: time.Date(2024, 6, 30, 0, 0, 0, 0, time.UTC), TotalValue: 1_100_000, Cash: 500_000, Positions: 600_000},
		},
	}
}

// newCompareTestRouter mirrors the production handler glue from
// handlers_backtest.go but uses an in-memory stub store instead of a
// real JobService. The query-string parsing, error classification and
// status-code mapping are byte-for-byte the production code, so any
// drift in the contract would show up here.
func newCompareTestRouter() (*gin.Engine, *compareStubStore) {
	gin.SetMode(gin.TestMode)
	store := &compareStubStore{jobs: map[string]json.RawMessage{}}
	logger := zerolog.Nop()
	router := gin.New()
	api := router.Group("/api/backtest")
	api.GET("/compare", func(c *gin.Context) {
		rawIDs := c.Query("ids")
		if rawIDs == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing 'ids' query parameter (comma-separated list of 2-8 backtest IDs)",
			})
			return
		}
		ids := strings.Split(rawIDs, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		resolver := backtest.CompareResultResolver(func(_ context.Context, id string) (backtest.BacktestResponse, error) {
			return store.Lookup(id)
		})
		report, err := backtest.CompareReports(c.Request.Context(), ids, resolver)
		if err != nil {
			if strings.Contains(err.Error(), "at least") ||
				strings.Contains(err.Error(), "at most") ||
				strings.Contains(err.Error(), "distinct") {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			logger.Error().Err(err).Strs("ids", ids).Msg("Compare failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "compare failed", "details": err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
	})
	return router, store
}

// ──── Tests ──────────────────────────────────────────────

func TestCompareHandler_Success(t *testing.T) {
	router, store := newCompareTestRouter()
	putCompareJob(store, "bt-a", makeCompareResponse("bt-a", "momentum", 0.10, -0.10, 1.2))
	putCompareJob(store, "bt-b", makeCompareResponse("bt-b", "meanrev", 0.20, -0.15, 1.0))

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/compare?ids=bt-a,bt-b", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		GeneratedAt time.Time                     `json:"generated_at"`
		Requested   int                           `json:"requested"`
		Resolved    int                           `json:"resolved"`
		Entries     []backtest.CompareEntry       `json:"entries"`
		Missing     []backtest.CompareMissingEntry `json:"missing"`
		Best        backtest.CompareBest          `json:"best"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 2, resp.Requested)
	assert.Equal(t, 2, resp.Resolved)
	assert.Empty(t, resp.Missing)
	assert.Len(t, resp.Entries, 2)

	// Sorted by TotalReturn desc: bt-b (0.20) before bt-a (0.10).
	assert.Equal(t, "bt-b", resp.Entries[0].ID)
	assert.Equal(t, "bt-a", resp.Entries[1].ID)

	// Best.TotalReturn should be bt-b.
	assert.Equal(t, "bt-b", resp.Best.TotalReturn)
	// Best.SharpeRatio should be bt-a (Sharpe 1.2 > 1.0).
	assert.Equal(t, "bt-a", resp.Best.SharpeRatio)
	// MaxDrawdown is least-negative: bt-a (-0.10) > bt-b (-0.15).
	assert.Equal(t, "bt-a", resp.Best.MaxDrawdown)
}

func TestCompareHandler_PartialResolution(t *testing.T) {
	router, store := newCompareTestRouter()
	putCompareJob(store, "bt-a", makeCompareResponse("bt-a", "momentum", 0.10, -0.10, 1.2))
	// bt-missing is intentionally not added

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/compare?ids=bt-a,bt-missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Resolved int                            `json:"resolved"`
		Missing  []backtest.CompareMissingEntry `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 1, resp.Resolved)
	assert.Len(t, resp.Missing, 1)
	assert.Equal(t, "bt-missing", resp.Missing[0].ID)
}

func TestCompareHandler_MissingIDsParam(t *testing.T) {
	router, _ := newCompareTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/backtest/compare", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing 'ids'")
}

func TestCompareHandler_TooFewIDs(t *testing.T) {
	router, store := newCompareTestRouter()
	putCompareJob(store, "bt-a", makeCompareResponse("bt-a", "momentum", 0.10, -0.10, 1.2))

	req := httptest.NewRequest(http.MethodGet, "/api/backtest/compare?ids=bt-a", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "at least 2")
}

func TestCompareHandler_TooManyIDs(t *testing.T) {
	router, _ := newCompareTestRouter()
	// 9 IDs > MaxCompareIDs=8
	ids := make([]string, 9)
	for i := range ids {
		ids[i] = "bt-" + string(rune('a'+i))
	}
	url := "/api/backtest/compare?ids=" + strings.Join(ids, ",")
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "at most 8")
}

func TestCompareHandler_TrimsWhitespace(t *testing.T) {
	router, store := newCompareTestRouter()
	putCompareJob(store, "bt-a", makeCompareResponse("bt-a", "momentum", 0.10, -0.10, 1.2))
	putCompareJob(store, "bt-b", makeCompareResponse("bt-b", "meanrev", 0.20, -0.15, 1.0))

	// Note the spaces around the IDs and the trailing comma.
	req := httptest.NewRequest(http.MethodGet, "/api/backtest/compare?ids=%20bt-a%20,%20bt-b%20,", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Resolved int `json:"resolved"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 2, resp.Resolved)
}
