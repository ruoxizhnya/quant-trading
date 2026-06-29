// Package main provides HTTP handlers for the multi-source data registry.
//
// These endpoints expose the source.Registry initialized in main()
// for observability and operational use. The endpoints are read-only
// — adapter lifecycle (enable/disable, swap) is currently a code change
// in main(). A future PR can add POST endpoints to mutate runtime state
// once persistence in data_source_registry is wired (see ADR-016).
package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
)

// registryCacheTTL is the freshness window for the cached /status payload.
// Health checks are expensive (network I/O), so we don't run them on every
// request. 30s is a reasonable trade-off.
const registryCacheTTL = 30 * time.Second

// registryStatus is the cached response for /api/datasource/registry/status.
type registryStatus struct {
	Adapters    []adapterStatusEntry `json:"adapters"`
	Chains      map[string][]string  `json:"chains"`
	GeneratedAt time.Time            `json:"generated_at"`
}

type adapterStatusEntry struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Enabled   bool     `json:"enabled"`
	Supported []string `json:"supported_types"`
	Healthy   *bool    `json:"healthy,omitempty"` // nil = not checked in this snapshot
	LastError string   `json:"last_error,omitempty"`
}

// registryHandler is the HTTP handler bundle for registry observability.
//
// Concurrency: statusCache is guarded by mu. The first request after
// GeneratedAt + cacheTTL re-runs HealthCheck on every adapter.
// HealthCheck is also available as a separate forced endpoint
// (/api/datasource/registry/health) that bypasses the cache.
//
// CR-02 (ODR-012): snapshotStatus does NOT hold mu across network I/O.
// The lock is acquired only for cache reads/writes; HealthCheck runs
// outside the critical section so a slow adapter does not block
// concurrent reads.
type registryHandler struct {
	reg         *source.Registry
	mu          sync.Mutex
	statusCache registryStatus
}

func newRegistryHandler(reg *source.Registry) *registryHandler {
	return &registryHandler{reg: reg}
}

// snapshotStatus builds a registryStatus from the current registry state.
// When force is false, the cached snapshot is reused if it is still fresh.
//
// Concurrency contract:
//   - The cache is consulted under h.mu (fast path).
//   - If the cache is stale/empty, h.mu is RELEASED before HealthCheck
//     runs. Multiple concurrent callers may all observe a stale cache
//     and enter the slow path simultaneously; this is acceptable because
//     (a) HealthCheck is idempotent and (b) the worst case is duplicated
//     network I/O, not a deadlock or starvation.
func (h *registryHandler) snapshotStatus(ctx context.Context, force bool) registryStatus {
	h.mu.Lock()
	if !force {
		fresh := !h.statusCache.GeneratedAt.IsZero() && time.Since(h.statusCache.GeneratedAt) < registryCacheTTL
		if fresh {
			cached := h.statusCache
			h.mu.Unlock()
			return cached
		}
	}
	h.mu.Unlock()

	// Slow path: build a fresh snapshot WITHOUT holding h.mu.
	health := h.reg.HealthCheck(ctx)
	names := h.reg.ListAdapters()
	entries := make([]adapterStatusEntry, 0, len(names))
	for _, n := range names {
		a := h.reg.GetAdapter(n)
		entry := adapterStatusEntry{
			Name:      a.Name(),
			Type:      string(a.Type()),
			Enabled:   a.Enabled(),
			Supported: a.SupportedTypes(),
		}
		if err, ok := health[n]; ok && err != nil {
			healthy := false
			entry.Healthy = &healthy
			entry.LastError = err.Error()
		} else if ok {
			healthy := true
			entry.Healthy = &healthy
		}
		entries = append(entries, entry)
	}

	fresh_status := registryStatus{
		Adapters:    entries,
		Chains:      h.reg.ListChains(),
		GeneratedAt: time.Now().UTC(),
	}

	h.mu.Lock()
	h.statusCache = fresh_status
	h.mu.Unlock()

	return fresh_status
}

// statusHandler — GET /api/datasource/registry
//
// Returns the list of registered adapters (with their supported types and
// enabled state) and the fallback chain for each data type. Health is
// checked lazily and cached for registryCacheTTL.
func (h *registryHandler) statusHandler(c *gin.Context) {
	snap := h.snapshotStatus(c.Request.Context(), false)
	c.JSON(http.StatusOK, snap)
}

// healthHandler — GET /api/datasource/registry/health
//
// Bypasses the status cache and runs a fresh HealthCheck on every
// registered adapter. Use this from ops dashboards / probes.
func (h *registryHandler) healthHandler(c *gin.Context) {
	// Use a short timeout so a single hanging adapter does not stall the probe.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	snap := h.snapshotStatus(ctx, true)
	c.JSON(http.StatusOK, gin.H{
		"healthy":      countHealthy(snap.Adapters),
		"unhealthy":    len(snap.Adapters) - countHealthy(snap.Adapters),
		"adapters":     snap.Adapters,
		"generated_at": snap.GeneratedAt,
	})
}

func countHealthy(entries []adapterStatusEntry) int {
	n := 0
	for _, e := range entries {
		if e.Healthy != nil && *e.Healthy {
			n++
		}
	}
	return n
}

// chainsHandler — GET /api/datasource/registry/chains
//
// Returns only the fallback chain table (lightweight; no health check).
func (h *registryHandler) chainsHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"chains": h.reg.ListChains(),
	})
}

// registerRegistryRoutes wires the registry endpoints on the router.
//
// The endpoints are mounted at /api/datasource/registry/* to avoid
// collision with the analysis-service /api/datasource/* routes (which
// use a different abstraction). The data-service registry endpoints
// are intentionally under a /registry subpath.
func registerRegistryRoutes(router *gin.Engine, h *registryHandler) {
	g := router.Group("/api/datasource/registry")
	{
		g.GET("/status", h.statusHandler)
		g.GET("/health", h.healthHandler)
		g.GET("/chains", h.chainsHandler)
	}
	logging.Logger.Info().Msg("Data source registry endpoints registered at /api/datasource/registry")
}
