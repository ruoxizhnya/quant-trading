package main

// Tests for the /sync/factors endpoints (AUD-15).
//
// The point of these tests is the response, not the computation: an empty
// fundamentals_detail used to come back as 200 "factor computed and cached"
// for a factor that had computed nothing. The computation side is pinned in
// pkg/data; what is pinned here is that the two doors tell the caller the
// truth — the single-factor door fails loudly, and the batch door names what
// it did not compute instead of claiming all eight ran.
//
// The router here is hand-wired rather than taken from main.go, so it pins the
// handlers and not the wiring.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// emptyFactorStore is a data.FactorStore with nothing in it: no OHLCV, no
// fundamentals snapshot and, the case under test, no fundamentals_detail rows.
type emptyFactorStore struct {
	saved int
}

func (s *emptyFactorStore) GetOHLCVForDateRange(context.Context, time.Time, time.Time) ([]domain.OHLCV, error) {
	return nil, nil
}

func (s *emptyFactorStore) GetFundamentalsSnapshot(context.Context, time.Time) ([]domain.FundamentalData, error) {
	return nil, nil
}

func (s *emptyFactorStore) GetFundamentalsDetailAsOf(context.Context, []string, time.Time) ([]domain.FundamentalsDetailRow, error) {
	return nil, nil
}

func (s *emptyFactorStore) GetTradingDays(context.Context, time.Time, time.Time) ([]time.Time, error) {
	return nil, nil
}

func (s *emptyFactorStore) SaveFactorCacheBatch(_ context.Context, entries []*domain.FactorCacheEntry) error {
	s.saved += len(entries)
	return nil
}

// factorRouter mirrors the two routes main.go registers.
func factorRouter(fc *data.FactorComputer) *gin.Engine {
	// gin 的 mode 由 TestMain 设一次（AUD-28）—— 别在这里调 gin.SetMode。
	r := gin.New()
	r.POST("/sync/factors/:factor_name", syncFactorHandler(fc))
	r.POST("/sync/factors/all", syncAllFactorsHandler(fc))
	return r
}

func postFactor(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"date":"20250101"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestSyncFactorHandler_EmptyFundamentalsFails(t *testing.T) {
	t.Parallel()
	store := &emptyFactorStore{}
	r := factorRouter(data.NewFactorComputer(store))

	// A vertical factor with nothing to read must not answer 200.
	w := postFactor(t, r, "/sync/factors/"+string(domain.FactorGrossMarginTrend))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("vertical factor status = %d, want 500 (body: %s)", w.Code, w.Body.String())
	}
	if store.saved != 0 {
		t.Errorf("saved %d entries, want 0", store.saved)
	}

	// Control: a cross-sectional factor on the same empty store still answers
	// 200. Without this the 500 above would prove nothing — it could just mean
	// every request fails.
	w = postFactor(t, r, "/sync/factors/"+string(domain.FactorMomentum))
	if w.Code != http.StatusOK {
		t.Errorf("momentum status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
}

func TestSyncAllFactorsHandler_ReportsSkipped(t *testing.T) {
	t.Parallel()
	store := &emptyFactorStore{}
	r := factorRouter(data.NewFactorComputer(store))

	w := postFactor(t, r, "/sync/factors/all")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}

	var body struct {
		Message  string   `json:"message"`
		Computed []string `json:"computed"`
		Skipped  []struct {
			Factor string `json:"factor"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, w.Body.String())
	}

	wantComputed := []string{"momentum", "value", "quality"}
	if len(body.Computed) != len(wantComputed) {
		t.Fatalf("computed = %v, want %v", body.Computed, wantComputed)
	}
	for i, want := range wantComputed {
		if body.Computed[i] != want {
			t.Errorf("computed[%d] = %q, want %q", i, body.Computed[i], want)
		}
	}

	const wantSkipped = 5
	if len(body.Skipped) != wantSkipped {
		t.Fatalf("skipped = %d, want %d: %+v", len(body.Skipped), wantSkipped, body.Skipped)
	}
	for _, s := range body.Skipped {
		if s.Reason == "" {
			t.Errorf("skipped %q has no reason", s.Factor)
		}
	}

	// The message must not claim success it did not have: with five of eight
	// factors skipped, "all factors computed and cached" is the sentence this
	// audit is about.
	if body.Message == "all factors computed and cached" {
		t.Errorf("message = %q, want it to admit the skips", body.Message)
	}
}
