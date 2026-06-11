// Package observability — HTTP handler for /metrics.
package observability

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns a gin.HandlerFunc that serves the Prometheus
// metrics in the supplied registry. Wrap it with router.GET("/metrics", ...)
//
// The handler is wrapped in promhttp.HandlerFor(...) rather than
// the global http.Handler to honour the package's "private
// registry" promise: only the four core metrics (and any
// additional collectors explicitly registered via
// RegisterCollectors) will be exposed.
func Handler(m *Metrics) gin.HandlerFunc {
	if m == nil {
		// Defensive fallback: return 503 so a misconfigured wire-up
		// is loud at runtime, not silent.
		return func(c *gin.Context) {
			c.String(503, "observability: nil metrics")
		}
	}
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	return gin.WrapH(h)
}

// MustCollect is a tiny helper around prometheus.Collector for the
// common case of constructing a process / Go runtime collector in
// main.go. Returns the value (no error) so it can be inlined into a
// RegisterCollectors(...) call site.
func MustCollect(c prometheus.Collector, err error) prometheus.Collector {
	if err != nil {
		panic(err)
	}
	return c
}
