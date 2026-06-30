package sync

import (
	"context"

	"github.com/ruoxizhnya/quant-trading/pkg/sync/types"
)

// Schedule is an alias for types.Schedule. S7-P1-2 (ODR-043): the
// canonical definition lives in pkg/sync/types to break the
// storage → sync reverse dependency.
type Schedule = types.Schedule

// ScheduleStore defines the interface for schedule persistence.
type ScheduleStore interface {
	CreateSyncSchedule(ctx context.Context, schedule *Schedule) error
	GetSyncSchedule(ctx context.Context, id int) (*Schedule, error)
	GetSyncScheduleByName(ctx context.Context, name string) (*Schedule, error)
	UpdateSyncSchedule(ctx context.Context, schedule *Schedule) error
	DeleteSyncSchedule(ctx context.Context, id int) error
	ListSyncSchedules(ctx context.Context, activeOnly bool) ([]*Schedule, error)
}
