// Package main provides HTTP handlers for data synchronization.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/sync"
)

// SyncHandler holds dependencies for sync-related HTTP handlers.
type SyncHandler struct {
	jobService    *sync.JobService
	queue         *sync.Queue
	workerPool    *sync.WorkerPool
	scheduler     *sync.Scheduler
	store         *storage.PostgresStore
	tushareClient *data.TushareClient
	dataCache     *data.DataCache
}

// NewSyncHandler creates a new SyncHandler.
func NewSyncHandler(store *storage.PostgresStore, tc *data.TushareClient, dc *data.DataCache) *SyncHandler {
	queue := sync.NewQueue(store)
	jobService := sync.NewJobService(store)
	// Wake idle workers when a job becomes pending outside Queue.Enqueue
	// (CreateJob / RetryJob write to the store directly). Without this,
	// workers blocked in WaitForJob never dequeue HTTP-created jobs.
	jobService.SetPendingNotifier(queue.NotifyJobAvailable)
	workerPool := sync.NewWorkerPool(queue, 3)
	// AUD-49: cancelling a running job has to reach the goroutine that is
	// executing it. Settling the row is not enough — the executor keeps going
	// until its context is cancelled, which is why the endpoint used to answer
	// 200 while `processed_items` kept climbing. This is the other half.
	jobService.SetRunningCanceller(workerPool.Cancel)
	scheduler := sync.NewScheduler(store, queue)

	// Register job types for scheduler
	scheduler.RegisterJobType("stocks", sync.JobTypeStocks)
	scheduler.RegisterJobType("ohlcv", sync.JobTypeOHLCV)
	scheduler.RegisterJobType("ohlcv_all", sync.JobTypeOHLCVAll)
	scheduler.RegisterJobType("fundamentals", sync.JobTypeFundamentals)
	scheduler.RegisterJobType("calendar", sync.JobTypeCalendar)
	scheduler.RegisterJobType("dividends", sync.JobTypeDividends)
	scheduler.RegisterJobType("splits", sync.JobTypeSplits)

	return &SyncHandler{
		jobService:    jobService,
		queue:         queue,
		workerPool:    workerPool,
		scheduler:     scheduler,
		store:         store,
		tushareClient: tc,
		dataCache:     dc,
	}
}

// StartWorkerPool starts the background worker pool.
func (h *SyncHandler) StartWorkerPool() {
	h.workerPool.Start()
}

// StopWorkerPool gracefully stops the worker pool.
func (h *SyncHandler) StopWorkerPool() {
	h.workerPool.Stop()
}

// RegisterExecutors registers all job executors.
func (h *SyncHandler) RegisterExecutors() {
	h.workerPool.RegisterExecutor(&stocksExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&ohlcvExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&ohlcvAllExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&fundamentalsExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&calendarExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&dividendsExecutor{tc: h.tushareClient, store: h.store})
	h.workerPool.RegisterExecutor(&splitsExecutor{tc: h.tushareClient, store: h.store})
}

// ---- Legacy Sync Endpoints (transformed to job creation) ----

// syncStocksHandler creates a job to sync stocks.
func (h *SyncHandler) syncStocksHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.StocksSyncParams
	if err := c.ShouldBindJSON(&req); err != nil {
		req.ListStatus = "L"
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeStocks, req)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "stocks sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
	})
}

// syncOHLCVHandler creates a job to sync OHLCV data.
func (h *SyncHandler) syncOHLCVHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.OHLCVSyncParams
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}

	if len(req.Symbols) == 0 {
		allStocks, err := h.store.GetAllStocks(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to fetch stocks from DB: ")
			return
		}
		for _, s := range allStocks {
			req.Symbols = append(req.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeOHLCV, req)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "OHLCV sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
		"symbols": len(req.Symbols),
	})
}

// syncAllOHLCVHandler creates a job to sync all OHLCV data.
func (h *SyncHandler) syncAllOHLCVHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.OHLCVSyncParams
	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults
	}
	if req.BatchSize <= 0 {
		req.BatchSize = 10
	}
	if req.EndDate == "" {
		req.EndDate = time.Now().Format("20060102")
	}
	if req.StartDate == "" {
		req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeOHLCVAll, req)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":    "bulk OHLCV sync job created",
		"job_id":     job.ID,
		"status":     job.Status,
		"start_date": req.StartDate,
		"end_date":   req.EndDate,
	})
}

// syncFundamentalsHandler creates a job to sync fundamentals data.
func (h *SyncHandler) syncFundamentalsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.FundamentalSyncParams
	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults - sync all stocks
	}

	if len(req.Symbols) == 0 {
		allStocks, err := h.store.GetAllStocks(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to fetch stocks from DB: ")
			return
		}
		for _, s := range allStocks {
			req.Symbols = append(req.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeFundamentals, req)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "fundamentals sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
		"symbols": len(req.Symbols),
	})
}

// syncCalendarHandler creates a job to sync trading calendar.
func (h *SyncHandler) syncCalendarHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.CalendarSyncParams
	if err := c.ShouldBindJSON(&req); err != nil {
		req.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		req.EndDate = time.Now().Format("20060102")
	}
	if req.Exchange == "" {
		req.Exchange = "both"
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeCalendar, req)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "calendar sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
	})
}

// syncDividendsHandler creates a job to sync dividends data.
func (h *SyncHandler) syncDividendsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req syncDividendsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults
	}

	params := sync.FundamentalSyncParams{
		Symbols: req.Symbols,
	}
	if len(params.Symbols) == 0 {
		allStocks, err := h.store.GetAllStocks(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to fetch stocks from DB: ")
			return
		}
		for _, s := range allStocks {
			params.Symbols = append(params.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeDividends, params)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "dividends sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
		"symbols": len(params.Symbols),
	})
}

// syncSplitsHandler creates a job to sync splits data.
func (h *SyncHandler) syncSplitsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req syncSplitsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Use defaults
	}

	params := sync.FundamentalSyncParams{
		Symbols: req.Symbols,
	}
	if len(params.Symbols) == 0 {
		allStocks, err := h.store.GetAllStocks(ctx)
		if err != nil {
			httpserver.Wrap(c, http.StatusInternalServerError, err, "failed to fetch stocks from DB: ")
			return
		}
		for _, s := range allStocks {
			params.Symbols = append(params.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeSplits, params)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "splits sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
		"symbols": len(params.Symbols),
	})
}

// ---- New REST API Endpoints ----

// listJobsHandler returns a list of sync jobs.
func (h *SyncHandler) listJobsHandler(c *gin.Context) {
	ctx := c.Request.Context()
	status := c.Query("status")
	limitStr := c.Query("limit")

	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	var jobs []*sync.Job
	var err error

	if status != "" {
		jobs, err = h.jobService.ListJobs(ctx, sync.JobStatus(status), limit)
	} else {
		jobs, err = h.jobService.ListJobs(ctx, "", limit)
	}

	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "count": len(jobs)})
}

// getJobHandler returns a single sync job by ID.
func (h *SyncHandler) getJobHandler(c *gin.Context) {
	ctx := c.Request.Context()
	jobID := c.Param("id")

	job, err := h.jobService.GetJob(ctx, jobID)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}
	if job == nil {
		httpserver.Fail(c, http.StatusNotFound, "job not found")
		return
	}

	c.JSON(http.StatusOK, job)
}

// cancelJobHandler cancels a sync job.
func (h *SyncHandler) cancelJobHandler(c *gin.Context) {
	ctx := c.Request.Context()
	jobID := c.Param("id")

	if err := h.jobService.CancelJob(ctx, jobID); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job cancelled", "job_id": jobID})
}

// retryJobHandler retries a failed sync job.
func (h *SyncHandler) retryJobHandler(c *gin.Context) {
	ctx := c.Request.Context()
	jobID := c.Param("id")

	job, err := h.jobService.RetryJob(ctx, jobID)
	if err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job queued for retry", "job_id": job.ID, "status": job.Status})
}

// getWorkerStatsHandler returns worker pool statistics.
func (h *SyncHandler) getWorkerStatsHandler(c *gin.Context) {
	stats := h.workerPool.Stats()
	c.JSON(http.StatusOK, stats)
}

// createJobHandler creates a sync job of any registered type — the typed
// door behind POST /api/sync/jobs (SPEC.md:1323, ODR-062 S-B). Unlike the
// legacy /sync/* endpoints above it, params are validated at the door so a
// mistyped body surfaces as 400 instead of an async job that can only fail
// later. One deliberate exception: an explicitly empty symbol list is
// accepted and no-ops (or fails) during execution, matching the legacy
// semantics.
func (h *SyncHandler) createJobHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req struct {
		Type   string          `json:"type"`
		Params json.RawMessage `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	if req.Type == "" {
		httpserver.Fail(c, http.StatusBadRequest, "type is required")
		return
	}
	// params may be absent — treat as an empty object.
	if len(req.Params) == 0 || string(req.Params) == "null" {
		req.Params = json.RawMessage("{}")
	}

	var job *sync.Job
	var err error

	switch sync.JobType(req.Type) {
	case sync.JobTypeStocks:
		var params sync.StocksSyncParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if params.ListStatus == "" {
			params.ListStatus = "L"
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobTypeStocks, params)

	case sync.JobTypeOHLCV:
		var params sync.OHLCVSyncParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		// The typed door requires an explicit symbol list (possibly empty —
		// it then no-ops during execution); whole-market sync has its own
		// job type (ohlcv_all). A missing key (vs an empty array) is
		// rejected so a mistyped body fails fast.
		if !jsonHasKey(req.Params, "symbols") {
			httpserver.Fail(c, http.StatusBadRequest, `params.symbols is required (use type "ohlcv_all" for whole-market sync)`)
			return
		}
		if !validDateRange(params.StartDate, params.EndDate) {
			httpserver.Fail(c, http.StatusBadRequest, "params.start_date/end_date must be YYYYMMDD or YYYY-MM-DD")
			return
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobTypeOHLCV, params)

	case sync.JobTypeOHLCVAll:
		var params sync.OHLCVSyncParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if params.BatchSize <= 0 {
			params.BatchSize = 10
		}
		if params.EndDate == "" {
			params.EndDate = time.Now().Format("20060102")
		}
		if params.StartDate == "" {
			params.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}
		if !validDateRange(params.StartDate, params.EndDate) {
			httpserver.Fail(c, http.StatusBadRequest, "params.start_date/end_date must be YYYYMMDD or YYYY-MM-DD")
			return
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobTypeOHLCVAll, params)

	case sync.JobTypeFundamentals:
		var params sync.FundamentalSyncParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if !jsonHasKey(req.Params, "symbols") {
			httpserver.Fail(c, http.StatusBadRequest, "params.symbols is required")
			return
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobTypeFundamentals, params)

	case sync.JobTypeCalendar:
		var params sync.CalendarSyncParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if params.Exchange == "" {
			params.Exchange = "both"
		}
		if params.StartDate == "" {
			params.StartDate = time.Now().AddDate(-1, 0, 0).Format("20060102")
		}
		if params.EndDate == "" {
			params.EndDate = time.Now().Format("20060102")
		}
		if !validDateRange(params.StartDate, params.EndDate) {
			httpserver.Fail(c, http.StatusBadRequest, "params.start_date/end_date must be YYYYMMDD or YYYY-MM-DD")
			return
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobTypeCalendar, params)

	case sync.JobTypeDividends, sync.JobTypeSplits:
		var params struct {
			Symbols []string `json:"symbols"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		if !jsonHasKey(req.Params, "symbols") {
			httpserver.Fail(c, http.StatusBadRequest, "params.symbols is required")
			return
		}
		job, err = h.jobService.CreateJob(ctx, sync.JobType(req.Type), sync.FundamentalSyncParams{Symbols: params.Symbols})

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("unknown job type %q; supported: stocks, ohlcv, ohlcv_all, fundamentals, calendar, dividends, splits", req.Type),
		})
		return
	}

	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": req.Type + " sync job created",
		"job_id":  job.ID,
		"status":  job.Status,
	})
}

// jsonHasKey reports whether the raw JSON object contains the key. Used to
// distinguish "field absent" (reject) from "field present but empty"
// (accept; no-op during execution).
func jsonHasKey(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// validDateRange validates an optional (start, end) date pair. Both formats
// in use across the repo are accepted: YYYYMMDD (tushare native) and
// YYYY-MM-DD (normalized by pkg/data.formatDate). Empty means "not provided".
func validDateRange(start, end string) bool {
	return validDate(start) && validDate(end)
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	if len(s) == 8 {
		_, err := time.Parse("20060102", s)
		return err == nil
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		_, err := time.Parse("2006-01-02", s)
		return err == nil
	}
	return false
}

// ---- Schedule API Endpoints ----

// createScheduleHandler creates a new sync schedule.
func (h *SyncHandler) createScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}

	if req.Name == "" || req.CronExpression == "" || req.JobType == "" {
		httpserver.Fail(c, http.StatusBadRequest, "name, cron_expression, and job_type are required")
		return
	}

	if err := h.scheduler.CreateSchedule(ctx, &req); err != nil {
		if errors.Is(err, sync.ErrInvalidCron) {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusCreated, req)
}

// listSchedulesHandler returns all sync schedules.
func (h *SyncHandler) listSchedulesHandler(c *gin.Context) {
	ctx := c.Request.Context()
	activeOnly := c.Query("active_only") == "true"

	schedules, err := h.scheduler.ListSchedules(ctx, activeOnly)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"schedules": schedules, "count": len(schedules)})
}

// getScheduleHandler returns a single schedule by ID.
func (h *SyncHandler) getScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid schedule ID")
		return
	}

	schedule, err := h.scheduler.GetSchedule(ctx, id)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}
	if schedule == nil {
		httpserver.Fail(c, http.StatusNotFound, "schedule not found")
		return
	}

	c.JSON(http.StatusOK, schedule)
}

// updateScheduleHandler updates an existing schedule.
func (h *SyncHandler) updateScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid schedule ID")
		return
	}

	var req sync.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}
	req.ID = id

	if err := h.scheduler.UpdateSchedule(ctx, &req); err != nil {
		if errors.Is(err, sync.ErrInvalidCron) {
			httpserver.Error(c, http.StatusBadRequest, err)
			return
		}
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, req)
}

// deleteScheduleHandler deletes a schedule by ID.
func (h *SyncHandler) deleteScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid schedule ID")
		return
	}

	if err := h.scheduler.DeleteSchedule(ctx, id); err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "schedule deleted", "id": id})
}

// toggleScheduleHandler activates or deactivates a schedule.
func (h *SyncHandler) toggleScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid schedule ID")
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpserver.Error(c, http.StatusBadRequest, err)
		return
	}

	if err := h.scheduler.ToggleSchedule(ctx, id, req.Active); err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "schedule updated", "id": id, "active": req.Active})
}

// runScheduleNowHandler manually triggers a schedule to run immediately.
func (h *SyncHandler) runScheduleNowHandler(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		httpserver.Fail(c, http.StatusBadRequest, "invalid schedule ID")
		return
	}

	job, err := h.scheduler.RunScheduleNow(ctx, id)
	if err != nil {
		httpserver.Error(c, http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "schedule triggered", "job_id": job.ID, "status": job.Status})
}

// getSchedulerStatsHandler returns scheduler statistics.
func (h *SyncHandler) getSchedulerStatsHandler(c *gin.Context) {
	stats := h.scheduler.Stats()
	c.JSON(http.StatusOK, stats)
}

// ---- SSE Progress Endpoint ----

// sseProgressHandler streams job progress updates via Server-Sent Events.
func (h *SyncHandler) sseProgressHandler(c *gin.Context) {
	ctx := c.Request.Context()
	jobID := c.Param("id")

	// Set SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Verify job exists
	job, err := h.jobService.GetJob(ctx, jobID)
	if err != nil {
		c.SSEvent("error", gin.H{"error": err.Error()})
		return
	}
	if job == nil {
		c.SSEvent("error", gin.H{"error": "job not found"})
		return
	}

	// Send initial state
	c.SSEvent("progress", job)
	c.Writer.Flush()

	// Poll for updates every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.SSEvent("close", gin.H{"message": "connection closed"})
			return
		case <-ticker.C:
			job, err := h.jobService.GetJob(ctx, jobID)
			if err != nil {
				c.SSEvent("error", gin.H{"error": err.Error()})
				c.Writer.Flush()
				continue
			}
			if job == nil {
				c.SSEvent("error", gin.H{"error": "job not found"})
				c.Writer.Flush()
				return
			}

			c.SSEvent("progress", job)
			c.Writer.Flush()

			// Stop if job is in terminal state
			if job.IsTerminal() {
				c.SSEvent("complete", job)
				c.Writer.Flush()
				return
			}
		}
	}
}

// ---- Job Executors ----

// stocksExecutor executes stock sync jobs.
type stocksExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *stocksExecutor) JobType() sync.JobType {
	return sync.JobTypeStocks
}

func (e *stocksExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.StocksSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// P2-4：一次同步拿全「在市 + 退市 + 暂停上市」三档。
	//
	// tushare stock_basic 的 list_status 一次只接受一档，而**只同步 L（在市）
	// 就是幸存者偏差的物理成因** —— 回测 2020 年时，2021 年退市的票根本
	// 不在库里，于是它最惨的那段行情永远不会出现在任何回测结果中。
	// "ALL" 与逗号分隔展开成 L/D/P 三次拉取，结果合并落库。
	statuses := expandListStatus(params.ListStatus)

	total := 0
	perStatus := make(map[string]int, len(statuses))
	for _, s := range statuses {
		stocks, err := e.tc.FetchStocks(ctx, params.Exchange, s)
		if err != nil {
			return nil, fmt.Errorf("fetch stocks (list_status=%s): %w", s, err)
		}
		total += len(stocks)
		perStatus[s] = len(stocks)
	}

	return map[string]any{
		"count":      total,
		"per_status": perStatus,
		// 明示这一批是否含退市票 —— 下游判断池子有没有幸存者偏差要看它。
		"includes_delisted": len(statuses) > 1,
	}, nil
}

// expandListStatus 把同步参数里的 list_status 展开成 tushare 能接受的单档列表。
//
//   - "" / "L" → 只在市（现状，保留默认行为）
//   - "ALL"    → L 上市 + D 退市 + P 暂停上市
//   - "L,P"    → 按逗号原样展开
func expandListStatus(v string) []string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "":
		return []string{"L"}
	case "ALL":
		return []string{"L", "D", "P"}
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"L"}
	}
	return out
}

// ohlcvExecutor executes OHLCV sync jobs.
type ohlcvExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *ohlcvExecutor) JobType() sync.JobType {
	return sync.JobTypeOHLCV
}

func (e *ohlcvExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.OHLCVSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	totalCount := 0
	for i, symbol := range params.Symbols {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		logging.Logger.Info().Str("symbol", symbol).Str("start", params.StartDate).Str("end", params.EndDate).Msg("fetching OHLCV")
		ohlcv, err := e.tc.FetchDailyOHLCV(ctx, symbol, params.StartDate, params.EndDate)
		if err != nil {
			logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync OHLCV")
			progress.ReportError(err.Error())
			continue
		}
		totalCount += len(ohlcv)
		progress.ReportProgress(i+1, len(params.Symbols), job.FailedItems)
	}

	return map[string]any{"count": totalCount, "symbols": len(params.Symbols)}, nil
}

// ohlcvAllExecutor executes bulk OHLCV sync jobs.
type ohlcvAllExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *ohlcvAllExecutor) JobType() sync.JobType {
	return sync.JobTypeOHLCVAll
}

func (e *ohlcvAllExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.OHLCVSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	stocks, err := e.store.GetAllStocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stocks: %w", err)
	}

	totalSynced := 0
	totalSkipped := 0
	totalFailed := 0

	for i := 0; i < len(stocks); i += params.BatchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + params.BatchSize
		if end > len(stocks) {
			end = len(stocks)
		}
		batch := stocks[i:end]

		for _, stock := range batch {
			if params.SkipExisting {
				hasData, err := e.store.HasOHLCVData(ctx, stock.Symbol)
				if err != nil {
					logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Error checking OHLCV data")
				}
				if hasData {
					totalSkipped++
					continue
				}
			}

			ohlcv, err := e.tc.FetchDailyOHLCV(ctx, stock.Symbol, params.StartDate, params.EndDate)
			if err != nil {
				totalFailed++
				logging.Logger.Warn().Err(err).Str("symbol", stock.Symbol).Msg("Failed to sync OHLCV")
				continue
			}
			totalSynced += len(ohlcv)
		}

		progress.ReportProgress(end, len(stocks), totalFailed)
	}

	return map[string]any{
		"total_stocks":   len(stocks),
		"records_synced": totalSynced,
		"skipped":        totalSkipped,
		"failed":         totalFailed,
	}, nil
}

// fundamentalsExecutor executes fundamentals sync jobs.
type fundamentalsExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *fundamentalsExecutor) JobType() sync.JobType {
	return sync.JobTypeFundamentals
}

func (e *fundamentalsExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.FundamentalSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	totalCount := 0
	totalSynced := 0
	totalFailed := 0

	batchSize := 10
	for i := 0; i < len(params.Symbols); i += batchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(params.Symbols) {
			end = len(params.Symbols)
		}
		batch := params.Symbols[i:end]

		for _, symbol := range batch {
			records, err := e.tc.FetchFundamentalsData(ctx, symbol, params.Date, params.Date)
			if err != nil {
				totalFailed++
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync fundamentals")
				continue
			}
			totalSynced++
			totalCount += len(records)
		}

		progress.ReportProgress(end, len(params.Symbols), totalFailed)
	}

	return map[string]any{
		"stocks_synced": totalSynced,
		"records_saved": totalCount,
		"failed_stocks": totalFailed,
	}, nil
}

// calendarCoverageTolerance 是「Tushare 返回的区间」与「请求的区间」之间允许的偏差。
//
// 为什么允许偏差而不是要求边界逐日对齐：`trade_cal` 返回的是**自然日**（含休市日），
// 实测正常时边界与请求完全一致；容差只用来兜住「请求边界落在已发布日历之外」
// （例如请求了尚未公布的年尾）这种正当情形。所以容差取小 —— 它要能抓住
// AUD-59 那种「整整少 269 天」的错区间，而不是给它留活路。
const calendarCoverageTolerance = 7 * 24 * time.Hour

// parseSyncDate 解析同步作业的日期参数，**两种格式都认**。
//
// 两种都必须认，因为 `validDateRange` 与它给出的报错文案
// `params.start_date/end_date must be YYYYMMDD or YYYY-MM-DD` 就是这么承诺的。
// 「承诺的格式集合」大于「实际支持的格式集合」正是 AUD-59：原日历执行器按 8 位
// 定长硬切 `[:4]`/`[4:6]`/`[6:8]`，于是 `2022-01-01` 被切成 `2022--01-`，
// Tushare 视作非法日期、回落到它自己的默认区间 —— **作业报 completed，
// 落库的却是错的日历**，全程零错误零告警。
func parseSyncDate(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, errors.New("date is required (YYYYMMDD or YYYY-MM-DD)")
	}
	for _, layout := range []string{"20060102", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q: expected YYYYMMDD or YYYY-MM-DD", raw)
}

// calendarFetchWindow 把作业参数解析成**唯一**的取数窗口。
//
// 「唯一」是承重的：取数用它，覆盖校验也用它。只要两处读的是同一组 time.Time，
// 就不可能再出现「请求 A 区间、校验 B 区间」这种脱钩 —— 而那正是复查 AUD-59 时
// 最容易再犯的形态（把硬切换个写法塞回来，校验却照着原始参数做，于是永远绿）。
func calendarFetchWindow(params sync.CalendarSyncParams) (start, end time.Time, err error) {
	start, err = parseSyncDate(params.StartDate)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start_date: %w", err)
	}
	end, err = parseSyncDate(params.EndDate)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end_date: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end_date %s is before start_date %s",
			end.Format("2006-01-02"), start.Format("2006-01-02"))
	}
	return start, end, nil
}

// calendarFetchArgs 返回**恰好**要传给 FetchTradingCalendar 的两个参数。
//
// 两个参数由**解析后的时间**派生，绝不在原始字符串上切片 —— 见 AUD-59。
// 单测用「带首尾空格的合法日期」把这条性质钉死：原始串不是 `20230101`，
// 但只要参数是从解析结果格式化的，取数参数就必然规范化为 `20230101`。
func calendarFetchArgs(params sync.CalendarSyncParams) (start, end time.Time, startArg, endArg string, err error) {
	start, end, err = calendarFetchWindow(params)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", err
	}
	return start, end, start.Format("20060102"), end.Format("20060102"), nil
}

// validateCalendarCoverage 断言「取回来的日历真的覆盖了请求的区间」。
//
// 为什么需要它：AUD-59 里作业报 `completed`、`count: 2922`，**零错误零告警**，
// 而库里只有请求区间的约五分之四 —— 一个「成功」的同步写进了错的数据。
// 回测只检查「区间内有没有交易日」（pkg/backtest/engine.go），**不检查区间覆盖
// 是否完整**，所以这种错日历会一路喂到回测里，而且从任何读数上都看不出来。
func validateCalendarCoverage(exchange string, reqStart, reqEnd time.Time, entries []storage.TradingCalendarEntry) error {
	if len(entries) == 0 {
		return fmt.Errorf("%s: tushare returned no calendar entries for %s..%s — "+
			"a sync that writes nothing must not report success",
			exchange, reqStart.Format("2006-01-02"), reqEnd.Format("2006-01-02"))
	}

	lo, hi := entries[0].TradeDate, entries[0].TradeDate
	for i := range entries {
		d := entries[i].TradeDate
		if d.Before(lo) {
			lo = d
		}
		if d.After(hi) {
			hi = d
		}
	}

	if lo.After(reqStart.Add(calendarCoverageTolerance)) || hi.Before(reqEnd.Add(-calendarCoverageTolerance)) {
		return fmt.Errorf("%s: returned calendar %s..%s does not cover the requested %s..%s — "+
			"refusing to persist a partial range; check the date format (YYYYMMDD vs YYYY-MM-DD)",
			exchange,
			lo.Format("2006-01-02"), hi.Format("2006-01-02"),
			reqStart.Format("2006-01-02"), reqEnd.Format("2006-01-02"))
	}
	return nil
}

// calendarExecutor executes calendar sync jobs.
type calendarExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *calendarExecutor) JobType() sync.JobType {
	return sync.JobTypeCalendar
}

func (e *calendarExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.CalendarSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// 取数窗口解析一次、两处共用（取数 + 覆盖校验）—— 见 calendarFetchArgs 的注释。
	start, end, startArg, endArg, err := calendarFetchArgs(params)
	if err != nil {
		return nil, err
	}

	exchanges := []string{params.Exchange}
	if params.Exchange == "both" {
		exchanges = []string{"SSE", "SZSE"}
	}

	var allEntries []storage.TradingCalendarEntry
	for _, exchange := range exchanges {
		entries, err := e.tc.FetchTradingCalendar(ctx, exchange, startArg, endArg)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s calendar: %w", exchange, err)
		}
		// 先校验、后落库：区间不对宁可让作业失败，也不能写进去（AUD-59）。
		if err := validateCalendarCoverage(exchange, start, end, entries); err != nil {
			return nil, err
		}
		allEntries = append(allEntries, entries...)
	}

	domainEntries := make([]*storage.TradingCalendarEntry, len(allEntries))
	for i := range allEntries {
		domainEntries[i] = &allEntries[i]
	}
	if err := e.store.SaveTradingCalendarBatch(ctx, domainEntries); err != nil {
		return nil, fmt.Errorf("failed to save calendar: %w", err)
	}

	return map[string]any{"count": len(allEntries)}, nil
}

// dividendsExecutor executes dividends sync jobs.
type dividendsExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *dividendsExecutor) JobType() sync.JobType {
	return sync.JobTypeDividends
}

func (e *dividendsExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.FundamentalSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	totalRecords := 0
	totalSynced := 0
	totalFailed := 0

	batchSize := 10
	for i := 0; i < len(params.Symbols); i += batchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(params.Symbols) {
			end = len(params.Symbols)
		}
		batch := params.Symbols[i:end]

		for _, symbol := range batch {
			records, err := e.tc.FetchDividends(ctx, symbol, "", "")
			if err != nil {
				totalFailed++
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync dividends")
				continue
			}
			totalSynced++
			totalRecords += len(records)
		}

		progress.ReportProgress(end, len(params.Symbols), totalFailed)
	}

	return map[string]any{
		"stocks_synced": totalSynced,
		"records_saved": totalRecords,
		"failed_stocks": totalFailed,
	}, nil
}

// splitsExecutor executes splits sync jobs.
type splitsExecutor struct {
	tc    *data.TushareClient
	store *storage.PostgresStore
}

func (e *splitsExecutor) JobType() sync.JobType {
	return sync.JobTypeSplits
}

func (e *splitsExecutor) Execute(ctx context.Context, job *sync.Job, progress sync.ProgressReporter) (any, error) {
	var params sync.FundamentalSyncParams
	if err := json.Unmarshal(job.Params, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	totalRecords := 0
	totalSynced := 0
	totalFailed := 0

	batchSize := 10
	for i := 0; i < len(params.Symbols); i += batchSize {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		end := i + batchSize
		if end > len(params.Symbols) {
			end = len(params.Symbols)
		}
		batch := params.Symbols[i:end]

		for _, symbol := range batch {
			records, err := e.tc.FetchSplits(ctx, symbol, "", "")
			if err != nil {
				totalFailed++
				logging.Logger.Warn().Err(err).Str("symbol", symbol).Msg("Failed to sync splits")
				continue
			}
			totalSynced++
			totalRecords += len(records)
		}

		progress.ReportProgress(end, len(params.Symbols), totalFailed)
	}

	return map[string]any{
		"stocks_synced": totalSynced,
		"records_saved": totalRecords,
		"failed_stocks": totalFailed,
	}, nil
}
