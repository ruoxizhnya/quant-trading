// Package sync provides a task queue and scheduler for data synchronization jobs.
package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/logging"
)

// Scheduler manages cron-based sync schedules and integrates with the job queue.
type Scheduler struct {
	cron     *cron.Cron
	store    ScheduleStore
	queue    *Queue
	logger   zerolog.Logger
	mu       sync.RWMutex
	entries  map[int]cron.EntryID // scheduleID -> cron entryID
	jobTypes map[string]JobType   // job type string -> JobType
}

// NewScheduler creates a new Scheduler.
func NewScheduler(store ScheduleStore, queue *Queue) *Scheduler {
	return &Scheduler{
		cron:     cron.New(),
		store:    store,
		queue:    queue,
		logger:   logging.WithContext(map[string]any{"component": "sync_scheduler"}),
		entries:  make(map[int]cron.EntryID),
		jobTypes: make(map[string]JobType),
	}
}

// RegisterJobType registers a job type for scheduling.
func (s *Scheduler) RegisterJobType(name string, jobType JobType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobTypes[name] = jobType
}

// Start begins the scheduler.
func (s *Scheduler) Start() error {
	s.logger.Info().Msg("Starting sync scheduler")

	// Load and schedule all active schedules from database
	if err := s.loadAndScheduleAll(context.Background()); err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	s.cron.Start()
	s.logger.Info().Msg("Sync scheduler started")
	return nil
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	s.logger.Info().Msg("Stopping sync scheduler")
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info().Msg("Sync scheduler stopped")
}

// CreateSchedule creates a new sync schedule and adds it to the cron.
func (s *Scheduler) CreateSchedule(ctx context.Context, schedule *Schedule) error {
	// Validate cron expression (supports both 5-field and 6-field formats)
	if _, err := cron.ParseStandard(schedule.CronExpression); err != nil {
		// Try with seconds if standard parsing fails
		parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(schedule.CronExpression); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	}

	// Set defaults
	if schedule.CreatedBy == "" {
		schedule.CreatedBy = "system"
	}

	// Save to database
	if err := s.store.CreateSyncSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to create schedule: %w", err)
	}

	// If active, schedule in cron
	if schedule.IsActive {
		if err := s.scheduleJob(schedule); err != nil {
			return fmt.Errorf("failed to schedule job: %w", err)
		}
	}

	s.logger.Info().
		Int("schedule_id", schedule.ID).
		Str("name", schedule.Name).
		Str("cron", schedule.CronExpression).
		Msg("Schedule created")

	return nil
}

// UpdateSchedule updates an existing schedule.
func (s *Scheduler) UpdateSchedule(ctx context.Context, schedule *Schedule) error {
	// Validate cron expression if provided
	if schedule.CronExpression != "" {
		if _, err := cron.ParseStandard(schedule.CronExpression); err != nil {
			// Try with seconds if standard parsing fails
			parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
			if _, err := parser.Parse(schedule.CronExpression); err != nil {
				return fmt.Errorf("invalid cron expression: %w", err)
			}
		}
	}

	// Remove existing cron entry
	s.mu.Lock()
	if entryID, ok := s.entries[schedule.ID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, schedule.ID)
	}
	s.mu.Unlock()

	// Update in database
	if err := s.store.UpdateSyncSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	// Re-schedule if active
	if schedule.IsActive {
		if err := s.scheduleJob(schedule); err != nil {
			return fmt.Errorf("failed to reschedule job: %w", err)
		}
	}

	s.logger.Info().
		Int("schedule_id", schedule.ID).
		Str("name", schedule.Name).
		Msg("Schedule updated")

	return nil
}

// DeleteSchedule deletes a schedule by ID.
func (s *Scheduler) DeleteSchedule(ctx context.Context, id int) error {
	// Remove from cron
	s.mu.Lock()
	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
	s.mu.Unlock()

	// Delete from database
	if err := s.store.DeleteSyncSchedule(ctx, id); err != nil {
		return fmt.Errorf("failed to delete schedule: %w", err)
	}

	s.logger.Info().Int("schedule_id", id).Msg("Schedule deleted")
	return nil
}

// GetSchedule retrieves a schedule by ID.
func (s *Scheduler) GetSchedule(ctx context.Context, id int) (*Schedule, error) {
	return s.store.GetSyncSchedule(ctx, id)
}

// GetScheduleByName retrieves a schedule by name.
func (s *Scheduler) GetScheduleByName(ctx context.Context, name string) (*Schedule, error) {
	return s.store.GetSyncScheduleByName(ctx, name)
}

// ListSchedules returns all schedules.
func (s *Scheduler) ListSchedules(ctx context.Context, activeOnly bool) ([]*Schedule, error) {
	return s.store.ListSyncSchedules(ctx, activeOnly)
}

// ToggleSchedule activates or deactivates a schedule.
func (s *Scheduler) ToggleSchedule(ctx context.Context, id int, active bool) error {
	schedule, err := s.store.GetSyncSchedule(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get schedule: %w", err)
	}
	if schedule == nil {
		return fmt.Errorf("schedule not found: %d", id)
	}

	schedule.IsActive = active

	// Remove existing cron entry
	s.mu.Lock()
	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
	s.mu.Unlock()

	// Re-schedule if activating
	if active {
		if err := s.scheduleJob(schedule); err != nil {
			return fmt.Errorf("failed to schedule job: %w", err)
		}
	}

	// Update in database
	if err := s.store.UpdateSyncSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	s.logger.Info().
		Int("schedule_id", id).
		Bool("active", active).
		Msg("Schedule toggled")

	return nil
}

// RunScheduleNow manually triggers a schedule to run immediately.
func (s *Scheduler) RunScheduleNow(ctx context.Context, id int) (*Job, error) {
	schedule, err := s.store.GetSyncSchedule(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule: %w", err)
	}
	if schedule == nil {
		return nil, fmt.Errorf("schedule not found: %d", id)
	}

	return s.createJobFromSchedule(ctx, schedule)
}

// loadAndScheduleAll loads all active schedules from the database and schedules them.
func (s *Scheduler) loadAndScheduleAll(ctx context.Context) error {
	schedules, err := s.store.ListSyncSchedules(ctx, true)
	if err != nil {
		return fmt.Errorf("failed to list schedules: %w", err)
	}

	for _, schedule := range schedules {
		if err := s.scheduleJob(schedule); err != nil {
			s.logger.Warn().Err(err).
				Int("schedule_id", schedule.ID).
				Str("name", schedule.Name).
				Msg("Failed to schedule job during load")
			continue
		}
	}

	s.logger.Info().Int("count", len(schedules)).Msg("Loaded and scheduled active schedules")
	return nil
}

// scheduleJob adds a schedule to the cron.
func (s *Scheduler) scheduleJob(schedule *Schedule) error {
	jobType := s.resolveJobType(schedule.JobType)
	if jobType == "" {
		return fmt.Errorf("unknown job type: %s", schedule.JobType)
	}

	entryID, err := s.cron.AddFunc(schedule.CronExpression, func() {
		s.runScheduledJob(schedule.ID)
	})
	if err != nil {
		return fmt.Errorf("failed to add cron job: %w", err)
	}

	s.mu.Lock()
	s.entries[schedule.ID] = entryID
	s.mu.Unlock()

	// Update next run time
	nextRun := s.cron.Entry(entryID).Next
	schedule.NextRunAt = &nextRun
	if err := s.store.UpdateSyncSchedule(context.Background(), schedule); err != nil {
		s.logger.Warn().Err(err).Int("schedule_id", schedule.ID).Msg("Failed to update next_run_at")
	}

	s.logger.Info().
		Int("schedule_id", schedule.ID).
		Str("name", schedule.Name).
		Str("cron", schedule.CronExpression).
		Time("next_run", nextRun).
		Msg("Job scheduled")

	return nil
}

// runScheduledJob is called by cron when a schedule triggers.
func (s *Scheduler) runScheduledJob(scheduleID int) {
	ctx := context.Background()

	schedule, err := s.store.GetSyncSchedule(ctx, scheduleID)
	if err != nil {
		s.logger.Error().Err(err).Int("schedule_id", scheduleID).Msg("Failed to get schedule for execution")
		return
	}
	if schedule == nil {
		s.logger.Error().Int("schedule_id", scheduleID).Msg("Schedule not found for execution")
		return
	}

	job, err := s.createJobFromSchedule(ctx, schedule)
	if err != nil {
		s.logger.Error().Err(err).Int("schedule_id", scheduleID).Msg("Failed to create job from schedule")
		return
	}

	// Update schedule with last run info
	now := time.Now()
	status := string(JobStatusPending)
	schedule.LastRunAt = &now
	schedule.LastRunStatus = &status
	schedule.LastRunJobID = &job.ID

	nextRun := s.cron.Entry(s.entries[scheduleID]).Next
	schedule.NextRunAt = &nextRun

	if err := s.store.UpdateSyncSchedule(ctx, schedule); err != nil {
		s.logger.Warn().Err(err).Int("schedule_id", scheduleID).Msg("Failed to update schedule after run")
	}

	s.logger.Info().
		Int("schedule_id", scheduleID).
		Str("job_id", job.ID).
		Str("job_type", string(job.JobType)).
		Msg("Scheduled job created")
}

// createJobFromSchedule creates a job from a schedule configuration.
func (s *Scheduler) createJobFromSchedule(ctx context.Context, schedule *Schedule) (*Job, error) {
	jobType := s.resolveJobType(schedule.JobType)
	if jobType == "" {
		return nil, fmt.Errorf("unknown job type: %s", schedule.JobType)
	}

	job := &Job{
		ID:         generateJobID(),
		JobType:    jobType,
		Status:     JobStatusPending,
		Params:     schedule.Params,
		CreatedAt:  time.Now(),
		MaxRetries: 3,
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to enqueue scheduled job: %w", err)
	}

	return job, nil
}

// resolveJobType resolves a string job type to JobType.
func (s *Scheduler) resolveJobType(name string) JobType {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if jobType, ok := s.jobTypes[name]; ok {
		return jobType
	}

	// Fallback to direct conversion
	return JobType(name)
}

// SchedulerStats holds statistics about the scheduler.
type SchedulerStats struct {
	ActiveSchedules int       `json:"active_schedules"`
	TotalEntries    int       `json:"total_entries"`
	NextRuns        []NextRun `json:"next_runs,omitempty"`
}

// NextRun represents the next scheduled run for a schedule.
type NextRun struct {
	ScheduleID int       `json:"schedule_id"`
	Name       string    `json:"name"`
	NextRun    time.Time `json:"next_run"`
}

// Stats returns current scheduler statistics.
func (s *Scheduler) Stats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := SchedulerStats{
		ActiveSchedules: len(s.entries),
		TotalEntries:    len(s.cron.Entries()),
	}

	for scheduleID, entryID := range s.entries {
		entry := s.cron.Entry(entryID)
		if !entry.Next.IsZero() {
			stats.NextRuns = append(stats.NextRuns, NextRun{
				ScheduleID: scheduleID,
				NextRun:    entry.Next,
			})
		}
	}

	return stats
}

// generateJobID generates a unique job ID.
func generateJobID() string {
	return fmt.Sprintf("scheduled-%d", time.Now().UnixNano())
}
