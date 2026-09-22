package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// registerProxyRoutes wires the analysis→data forwarding surface. The viper
// instance is the loaded analysis config (not the global one) so that
// data_service.url is actually read — the global viper was never populated
// by loadConfig, which meant the hardcoded docker hostname silently won
// (ODR-062 S-B wiring fix).
func registerProxyRoutes(router *gin.Engine, httpClient *http.Client, v *viper.Viper, logger zerolog.Logger) {
	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://data-service:8081"
	}

	// ODR-062 (S-B): gateway proxy for the L0 sync-job management API
	// (/api/sync/jobs*, /api/sync/workers, /api/sync/schedules*) — the
	// contract documented in SPEC.md:1321-1334, consumed by the SPA
	// (web/src/api/sync.ts) and the e2e data-sync suites. Uses a streaming
	// ReverseProxy rather than proxyRequest because the family includes an
	// SSE endpoint (GET /api/sync/jobs/:id/progress) that must not be
	// buffered.
	target, err := url.Parse(dataServiceURL)
	if err != nil {
		logger.Error().Err(err).Str("url", dataServiceURL).Msg("invalid data_service.url; data proxy routes not registered")
		return
	}
	syncProxy := httputil.NewSingleHostReverseProxy(target)
	syncProxy.FlushInterval = -1 // flush every write immediately — required for SSE
	router.Any("/api/sync/*path", func(c *gin.Context) {
		syncProxy.ServeHTTP(c.Writer, c.Request)
	})

	proxyRequest := func(c *gin.Context, method, targetURL string, body io.Reader) {
		var resp *http.Response
		var err error
		switch method {
		case http.MethodGet:
			resp, err = httpClient.Get(targetURL)
		case http.MethodPost:
			resp, err = httpClient.Post(targetURL, "application/json", body)
		default:
			req, reqErr := http.NewRequestWithContext(c.Request.Context(), method, targetURL, body)
			if reqErr != nil {
				httpserver.Fail(c, http.StatusBadGateway, "failed to create proxy request")
				return
			}
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err = httpClient.Do(req)
		}
		if err != nil {
			httpserver.Wrap(c, http.StatusBadGateway, err, "data service unavailable: ")
			return
		}
		defer resp.Body.Close()
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			httpserver.Fail(c, http.StatusBadGateway, "invalid response from data service")
			return
		}
		c.JSON(resp.StatusCode, result)
	}

	router.GET("/api/ohlcv/:symbol", func(c *gin.Context) {
		symbol := c.Param("symbol")
		startDate := c.Query("start_date")
		endDate := c.Query("end_date")
		if startDate == "" || endDate == "" {
			httpserver.Fail(c, http.StatusBadRequest, "start_date and end_date required (YYYYMMDD)")
			return
		}
		dataURL := fmt.Sprintf("%s/ohlcv/%s?start_date=%s&end_date=%s", dataServiceURL, symbol, startDate, endDate)
		proxyRequest(c, http.MethodGet, dataURL, nil)
	})

	router.POST("/api/screen", func(c *gin.Context) {
		var reqBody map[string]interface{}
		if err := json.NewDecoder(c.Request.Body).Decode(&reqBody); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "invalid request body")
			return
		}
		bodyBytes, _ := json.Marshal(reqBody)
		proxyRequest(c, http.MethodPost, dataServiceURL+"/screen", bytes.NewReader(bodyBytes))
	})

	router.GET("/api/stocks/count", func(c *gin.Context) {
		proxyRequest(c, http.MethodGet, dataServiceURL+"/stocks/count", nil)
	})

	router.GET("/api/market/index", func(c *gin.Context) {
		proxyRequest(c, http.MethodGet, dataServiceURL+"/market/index", nil)
	})

	// TASKS.md P5-3: the citation-coordinate read. GET /api/factors/:name is
	// the factor_cache read whose rows carry the ADR-022 §5 five-tuple
	// {source, dataset, key, as_of, content_hash} (cmd/data/handlers_factor.go).
	// The L0 route has no /api prefix (/factors/:factor_name, cmd/data/main.go),
	// so this facade both supplies the prefix the SPA uses and forwards
	// symbol/date verbatim. Status is passed through unchanged because 404 is
	// the first-class "no such factor_cache row" answer, and a hash-only
	// citation (response never archived) must reach the SPA intact rather than
	// being rewritten into an error here.
	router.GET("/api/factors/:factor_name", func(c *gin.Context) {
		query := url.Values{}
		if symbol := c.Query("symbol"); symbol != "" {
			query.Set("symbol", symbol)
		}
		if date := c.Query("date"); date != "" {
			query.Set("date", date)
		}
		dataURL := fmt.Sprintf("%s/factors/%s", dataServiceURL, url.PathEscape(c.Param("factor_name")))
		if encoded := query.Encode(); encoded != "" {
			dataURL += "?" + encoded
		}
		proxyRequest(c, http.MethodGet, dataURL, nil)
	})

	// ODR-062 (S-C): removed as dead code (zero consumers across web/src,
	// e2e, static pages and Go internals — Go side reaches L0 directly):
	//   POST /api/sync/calendar, POST /sync/calendar (mirror),
	//   GET  /api/v1/trading/calendar.
	// The 4 legacy no-prefix mirrors (/ohlcv/:symbol, /screen, /stocks/count,
	// /market/index) that used to sit here are gone as well — AUD-33
	// (2026-09-22) retired the legacy pages they existed for. They were never
	// anything but a convenience for cmd/analysis/static/*.html; the SPA calls
	// the /api-prefixed routes above (web/src/api/market.ts).
	// deps_test.go asserts these paths stay unregistered.
}
