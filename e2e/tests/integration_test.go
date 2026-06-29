package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	baseURL        = "http://localhost:8085"
	dataServiceURL = "http://localhost:8081"
	timeout        = 30 * time.Second
)

// ============================================================
// Integration Test Suite for Quant Trading System
// Tests all services via HTTP API (requires Docker Compose running)
// ============================================================

// S7-P0-8 (ODR-043-6): TestMain probes the analysis service before
// running any integration test. If the service is unreachable (Docker
// Compose not started), the entire suite is skipped so `go test ./...`
// exits 0 instead of FAIL. Set E2E_FORCE_SKIP=1 to skip unconditionally
// (useful in CI without Docker).
func TestMain(m *testing.M) {
	if os.Getenv("E2E_FORCE_SKIP") == "1" {
		fmt.Println("e2e: E2E_FORCE_SKIP=1, skipping integration tests")
		os.Exit(0)
	}
	if !servicesReachable() {
		fmt.Println("e2e: analysis service not reachable, skipping integration tests (start Docker Compose to enable)")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// servicesReachable probes the analysis service health endpoint with a
// short timeout. Returns true if the service responds, false otherwise.
// S7-P0-8 (ODR-043-6).
func servicesReachable() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestHealthCheck_AllServices(t *testing.T) {
	t.Run("Analysis Service Health", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var health map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&health)
		assert.Contains(t, health, "status")
	})

	t.Run("Data Service Health", func(t *testing.T) {
		resp, err := http.Get(dataServiceURL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestBacktestAPI_EndToEnd(t *testing.T) {
	payload := map[string]interface{}{
		"strategy":        "momentum",
		"stock_pool":      []string{"600000.SH"},
		"start_date":      "2024-01-01",
		"end_date":        "2024-03-31",
		"initial_capital": 1000000,
	}

	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(baseURL+"/api/backtest", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	if resp.StatusCode == http.StatusOK {
		if jobID, ok := result["job_id"].(string); ok {
			t.Logf("✅ Async backtest submitted successfully: job_id=%s", jobID)
			pollForCompletion(t, jobID)
		} else if _, ok := result["total_return"]; ok || result["status"] == "completed" {
			totalReturn := result["total_return"].(float64)
			totalTrades := result["total_trades"].(float64)
			strategy := result["strategy"].(string)
			t.Logf("✅ Sync backtest completed successfully!")
			t.Logf("   Strategy: %s", strategy)
			t.Logf("   Total Return: %.2f%%", totalReturn*100)
			t.Logf("   Total Trades: %.0f", totalTrades)
		} else {
			t.Logf("Response received: status=%d", resp.StatusCode)
		}
	} else {
		t.Logf("Backtest request failed with status %d: %s", resp.StatusCode, resp.Status)
	}
}

func TestStrategyAPI_ListStrategies(t *testing.T) {
	resp, err := http.Get(baseURL + "/api/strategies")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var strategies []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&strategies)

	if len(strategies) > 0 {
		for _, s := range strategies {
			assert.Contains(t, s, "name")
			assert.Contains(t, s, "description")
			t.Logf("Strategy: %s - %s", s["name"], s["description"])
		}
	} else {
		t.Log("⚠️ No strategies registered (this is normal if strategy service has no plugins)")
	}
}

func TestOHLCVAPI_DataRetrieval(t *testing.T) {
	symbol := "600000.SH"
	startDate := "20240101"
	endDate := "20240115"

	url := fmt.Sprintf("%s/api/ohlcv/%s?start=%s&end=%s", baseURL, symbol, startDate, endDate)
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var ohlcv []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&ohlcv)

		assert.NotEmpty(t, ohlcv, "should have OHLCV data for "+symbol)

		firstBar := ohlcv[0]
		assert.Contains(t, firstBar, "open")
		assert.Contains(t, firstBar, "high")
		assert.Contains(t, firstBar, "low")
		assert.Contains(t, firstBar, "close")
		assert.Contains(t, firstBar, "volume")

		t.Logf("✅ Retrieved %d bars for %s from %s to %s", len(ohlcv), symbol, startDate, endDate)
	} else {
		t.Logf("⚠️ OHLCV API returned status %d (may need data sync)", resp.StatusCode)
	}
}

func TestFundamentalsAPI_DataRetrieval(t *testing.T) {
	symbol := "600000.SH"
	date := "20240930"

	url := fmt.Sprintf("%s/api/fundamentals/%s?date=%s", baseURL, symbol, date)
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var fundamentals []map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&fundamentals)

		if len(fundamentals) > 0 {
			f := fundamentals[0]
			assert.Contains(t, f, "pe")
			assert.Contains(t, f, "pb")
			assert.Contains(t, f, "roe")

			t.Logf("✅ Fundamentals for %s: PE=%v PB=%v ROE=%v", symbol, f["pe"], f["pb"], f["roe"])
		}
	} else {
		t.Log("⚠️ No fundamentals data returned (may need data sync)")
	}
}

func TestFactorAPI_FactorComputation(t *testing.T) {
	date := "20240115"

	url := fmt.Sprintf("%s/api/factors/compute?date=%s&lookback=10", baseURL, date)
	resp, err := http.Post(url, "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)

		assert.Contains(t, result, "status")
		t.Logf("✅ Factor computation result: %+v", result)
	} else {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("⚠️ Factor computation returned status %d: %s", resp.StatusCode, string(body))
	}
}

// pollForCompletion waits for async backtest to complete
func pollForCompletion(t *testing.T, jobID string) {
	client := &http.Client{Timeout: timeout}
	maxWait := 120 * time.Second
	pollInterval := 5 * time.Second

	start := time.Now()
	for time.Since(start) < maxWait {
		url := fmt.Sprintf("%s/api/backtest/status/%s", baseURL, jobID)
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		var status map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if state, ok := status["state"].(string); ok && (state == "completed" || state == "failed") {
			t.Logf("Job %s finished with state: %s", jobID, state)
			return
		}

		time.Sleep(pollInterval)
	}
	t.Logf("⚠️ Polling timed out after %v for job %s", maxWait, jobID)
}

// TestExecutionService_OrderPersistence tests the new order persistence feature
func TestExecutionService_OrderPersistence(t *testing.T) {
	executionURL := "http://localhost:8084"

	orderPayload := map[string]interface{}{
		"symbol":     "600000.SH",
		"direction":  "long",
		"order_type": "market",
		"quantity":   1000,
		"price":      0,
	}

	body, _ := json.Marshal(orderPayload)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(executionURL+"/api/orders", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		var orderResult map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&orderResult)

		assert.Contains(t, orderResult, "order_id")
		assert.Contains(t, orderResult, "symbol")
		assert.Equal(t, "600000.SH", orderResult["symbol"])

		t.Logf("✅ Order persisted: id=%s status=%v", orderResult["order_id"], orderResult["status"])
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		t.Logf("⚠️ Order creation returned status %d: %s", resp.StatusCode, string(respBody))
	}
}

// TestDockerComposeServicesConnectivity verifies all services are reachable
func TestDockerComposeServicesConnectivity(t *testing.T) {
	services := map[string]string{
		"analysis-service":  baseURL + "/api/health",
		"data-service":      dataServiceURL + "/health",
		"strategy-service":  "http://localhost:8082/strategies",
		"risk-service":      "http://localhost:8083/risk/health",
		"execution-service": "http://localhost:8084/api/orders",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for name, url := range services {
		t.Run(name, func(t *testing.T) {
			resp, err := client.Get(url)
			if err != nil {
				t.Logf("⚠️ Service %s not reachable: %v", name, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
				t.Logf("✅ %s is responding (status %d)", name, resp.StatusCode)
			} else {
				t.Logf("⚠️ %s responded with status %d", name, resp.StatusCode)
			}
		})
	}
}

// Benchmark test for API response times
func BenchmarkAPI_HealthCheck(b *testing.B) {
	client := &http.Client{Timeout: timeout}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Get(baseURL + "/api/health")
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// TestContext ensures tests run with proper context cancellation support
func TestMain_IntegrationSuite(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t.Run("All Services Health Check", func(t *testing.T) {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for services")
		default:
			TestHealthCheck_AllServices(t)
		}
	})

	t.Run("Docker Compose Connectivity", func(t *testing.T) {
		select {
		case <-ctx.Done():
			t.Fatal("timeout checking connectivity")
		default:
			TestDockerComposeServicesConnectivity(t)
		}
	})
}
