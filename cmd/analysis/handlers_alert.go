package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ruoxizhnya/quant-trading/pkg/alert"
)

// alertHistoryResponse is the wire shape returned by
// GET /api/alerts/history. We wrap the alerts slice so future fields
// (e.g. next cursor, total count) can be added without breaking
// clients.
type alertHistoryResponse struct {
	Alerts []alert.Alert `json:"alerts"`
	Count  int           `json:"count"`
	Limit  int           `json:"limit"`
}

// alertStatsResponse aggregates the AlertManager's running counters
// plus the recorder's lifetime state. Operators use this to confirm
// the loop is alive (recorder.Len > 0 over time) and to watch
// per-rule rates via webhook delivery logs.
type alertStatsResponse struct {
	Enabled         bool            `json:"enabled"`
	ChannelCount    int             `json:"channel_count"`
	RecorderLen     int             `json:"recorder_len"`
	RecorderEvicted uint64          `json:"recorder_evicted"`
	HistoryLen      int             `json:"history_len"`
	HistoryLimit    int             `json:"history_limit"`
	ByRule          map[string]int  `json:"by_rule"`
	BySeverity      map[string]int  `json:"by_severity"`
}

// alertsRecentHandler returns the most recent N alerts from the
// AlertHistory ring buffer. Query parameters:
//
//	limit     int   1..history_limit, default 50
//	severity  string  "info" | "warning" | "critical" — optional filter
func alertsRecentHandler(loop *PeriodicAlertLoop) gin.HandlerFunc {
	return func(c *gin.Context) {
		history := loop.History()
		limit := 50
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}
		// Cap at the buffer capacity so we never over-allocate.
		if limit > 1000 {
			limit = 1000
		}

		var all []alert.Alert
		if sev := c.Query("severity"); sev != "" {
			all = history.FilterBySeverity(alert.Severity(sev))
		} else {
			all = history.Snapshot()
		}
		if len(all) > limit {
			all = all[:limit]
		}

		c.JSON(http.StatusOK, alertHistoryResponse{
			Alerts: all,
			Count:  len(all),
			Limit:  limit,
		})
	}
}

// alertsForceCheckHandler runs an immediate evaluation cycle and
// returns the number of alerts dispatched. This is useful for
// operators who want to verify the loop is healthy without
// waiting for the next tick.
//
// On error, returns 500 with a JSON body describing the failure.
func alertsForceCheckHandler(loop *PeriodicAlertLoop) gin.HandlerFunc {
	return func(c *gin.Context) {
		n, err := loop.TriggerOnce(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":  "alert evaluation failed",
				"detail": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"dispatched": n,
		})
	}
}

// alertsStatsHandler returns the AlertManager + recorder + history
// state. Read-only; useful for /api/health dashboards and for
// debugging "why aren't alerts firing?".
func alertsStatsHandler(loop *PeriodicAlertLoop) gin.HandlerFunc {
	return func(c *gin.Context) {
		am := loop.AlertManager()
		history := loop.History()

		// Roll up by rule and by severity from the in-memory history.
		byRule := map[string]int{}
		bySeverity := map[string]int{}
		for _, a := range history.Snapshot() {
			byRule[a.Rule]++
			bySeverity[string(a.Severity)]++
		}

		resp := alertStatsResponse{
			ChannelCount: am.ChannelCount(),
			HistoryLen:   history.Len(),
		}
		if rec, ok := am.Recorder(); ok {
			resp.RecorderLen = rec.Len()
			resp.RecorderEvicted = rec.Evicted()
		}
		// We don't currently track HistoryLimit on the struct itself;
		// the HTTP layer can read it from the configuration. For now
		// we report the live len; "limit" remains 0 unless main.go
		// wires the limit through (P2-3 candidate).
		resp.ByRule = byRule
		resp.BySeverity = bySeverity

		c.JSON(http.StatusOK, resp)
	}
}

// registerAlertRoutes attaches the alert HTTP endpoints to the
// supplied router. All endpoints are mounted under /api/alerts/.
//
// This function is wired in main.go alongside registerCopilotRoutes
// and registerAuthRoutes; see registerRoutes below.
func registerAlertRoutes(router gin.IRouter, loop *PeriodicAlertLoop) {
	group := router.Group("/api/alerts")
	group.GET("/history", alertsRecentHandler(loop))
	group.POST("/force-check", alertsForceCheckHandler(loop))
	group.GET("/stats", alertsStatsHandler(loop))
}
