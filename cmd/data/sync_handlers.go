// Package main provides HTTP handlers for data synchronization.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
	workerPool := sync.NewWorkerPool(queue, 3)
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.Symbols) == 0 {
		allStocks, err := h.store.GetAllStocks(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
			return
		}
		for _, s := range allStocks {
			req.Symbols = append(req.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeOHLCV, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "OHLCV sync job created",
		"job_id":   job.ID,
		"status":   job.Status,
		"symbols":  len(req.Symbols),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
			return
		}
		for _, s := range allStocks {
			req.Symbols = append(req.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeFundamentals, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
			return
		}
		for _, s := range allStocks {
			params.Symbols = append(params.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeDividends, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stocks from DB: " + err.Error()})
			return
		}
		for _, s := range allStocks {
			params.Symbols = append(params.Symbols, s.Symbol)
		}
	}

	job, err := h.jobService.CreateJob(ctx, sync.JobTypeSplits, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if job == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}

// cancelJobHandler cancels a sync job.
func (h *SyncHandler) cancelJobHandler(c *gin.Context) {
	ctx := c.Request.Context()
	jobID := c.Param("id")

	if err := h.jobService.CancelJob(ctx, jobID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "job queued for retry", "job_id": job.ID, "status": job.Status})
}

// getWorkerStatsHandler returns worker pool statistics.
func (h *SyncHandler) getWorkerStatsHandler(c *gin.Context) {
	stats := h.workerPool.Stats()
	c.JSON(http.StatusOK, stats)
}

// ---- Schedule API Endpoints ----

// createScheduleHandler creates a new sync schedule.
func (h *SyncHandler) createScheduleHandler(c *gin.Context) {
	ctx := c.Request.Context()
	var req sync.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name == "" || req.CronExpression == "" || req.JobType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name, cron_expression, and job_type are required"})
		return
	}

	if err := h.scheduler.CreateSchedule(ctx, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule ID"})
		return
	}

	schedule, err := h.scheduler.GetSchedule(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if schedule == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule ID"})
		return
	}

	var req sync.Schedule
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ID = id

	if err := h.scheduler.UpdateSchedule(ctx, &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule ID"})
		return
	}

	if err := h.scheduler.DeleteSchedule(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule ID"})
		return
	}

	var req struct {
		Active bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.scheduler.ToggleSchedule(ctx, id, req.Active); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid schedule ID"})
		return
	}

	job, err := h.scheduler.RunScheduleNow(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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

	stocks, err := e.tc.FetchStocks(ctx, params.Exchange, params.ListStatus)
	if err != nil {
		return nil, err
	}

	return map[string]any{"count": len(stocks)}, nil
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

	startFormatted := fmt.Sprintf("%s-%s-%s", params.StartDate[:4], params.StartDate[4:6], params.StartDate[6:8])
	endFormatted := fmt.Sprintf("%s-%s-%s", params.EndDate[:4], params.EndDate[4:6], params.EndDate[6:8])

	exchanges := []string{params.Exchange}
	if params.Exchange == "both" {
		exchanges = []string{"SSE", "SZSE"}
	}

	var allEntries []storage.TradingCalendarEntry
	for _, exchange := range exchanges {
		entries, err := e.tc.FetchTradingCalendar(ctx, exchange, startFormatted, endFormatted)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch %s calendar: %w", exchange, err)
		}
		allEntries = append(allEntries, entries...)
	}

	if len(allEntries) > 0 {
		domainEntries := make([]*storage.TradingCalendarEntry, len(allEntries))
		for i := range allEntries {
			domainEntries[i] = &allEntries[i]
		}
		if err := e.store.SaveTradingCalendarBatch(ctx, domainEntries); err != nil {
			return nil, fmt.Errorf("failed to save calendar: %w", err)
		}
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
