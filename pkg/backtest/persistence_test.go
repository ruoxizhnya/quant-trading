package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ────────────────────────────────────────────────────

func newTestStore(t *testing.T) *DiskStateStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewDiskStateStore(dir)
	require.NoError(t, err)
	return store
}

func sampleSnapshot(id string) BacktestStateSnapshot {
	return BacktestStateSnapshot{
		ID:          id,
		Status:      "completed",
		Params:      domain.BacktestParams{StrategyName: "momentum", InitialCapital: 100000},
		Result:      &domain.BacktestResult{TotalReturn: 0.15, TotalTrades: 42},
		StartedAt:   time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC),
		CompletedAt: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		Frozen:      true,
	}
}

// ─── Construction tests ──────────────────────────────────────────

func TestNewDiskStateStore_CreatesDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "subdir", "states")
	store, err := NewDiskStateStore(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, store.Dir())

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewDiskStateStore_EmptyDir(t *testing.T) {
	t.Parallel()
	_, err := NewDiskStateStore("")
	assert.Error(t, err)
}

// ─── Save / Load round-trip tests ───────────────────────────────

func TestDiskStateStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	original := sampleSnapshot("bt-001")

	err := store.Save(ctx, original)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "bt-001")
	require.NoError(t, err)

	assert.Equal(t, original.ID, loaded.ID)
	assert.Equal(t, original.Status, loaded.Status)
	assert.Equal(t, original.Params.StrategyName, loaded.Params.StrategyName)
	assert.Equal(t, original.Params.InitialCapital, loaded.Params.InitialCapital)
	assert.Equal(t, original.StartedAt, loaded.StartedAt)
	assert.Equal(t, original.CompletedAt, loaded.CompletedAt)
	assert.Equal(t, original.Frozen, loaded.Frozen)
}

func TestDiskStateStore_LoadedMatchesOriginal(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	original := sampleSnapshot("bt-002")
	original.Result = &domain.BacktestResult{
		TotalReturn:  0.2345,
		AnnualReturn: 0.5678,
		SharpeRatio:  1.89,
		MaxDrawdown:  -0.12,
		WinRate:      0.55,
		TotalTrades:  100,
		WinTrades:    55,
		LoseTrades:   45,
		Trades: []domain.Trade{
			{ID: "t1", Symbol: "AAPL", Direction: "long", Quantity: 100, Price: 150.25},
		},
		PortfolioValues: []domain.PortfolioValue{
			{Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), TotalValue: 100000, Cash: 50000},
		},
	}

	err := store.Save(ctx, original)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "bt-002")
	require.NoError(t, err)

	require.NotNil(t, loaded.Result)
	assert.Equal(t, original.Result.TotalReturn, loaded.Result.TotalReturn)
	assert.Equal(t, original.Result.AnnualReturn, loaded.Result.AnnualReturn)
	assert.Equal(t, original.Result.SharpeRatio, loaded.Result.SharpeRatio)
	assert.Equal(t, original.Result.MaxDrawdown, loaded.Result.MaxDrawdown)
	assert.Equal(t, original.Result.WinRate, loaded.Result.WinRate)
	assert.Equal(t, original.Result.TotalTrades, loaded.Result.TotalTrades)
	require.Len(t, loaded.Result.Trades, 1)
	assert.Equal(t, "AAPL", loaded.Result.Trades[0].Symbol)
	require.Len(t, loaded.Result.PortfolioValues, 1)
	assert.Equal(t, float64(100000), loaded.Result.PortfolioValues[0].TotalValue)
}

func TestDiskStateStore_SaveWithError(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	original := sampleSnapshot("bt-err")
	original.Status = "failed"
	original.Error = errors.New("insufficient data for 000001.SZ")

	err := store.Save(ctx, original)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "bt-err")
	require.NoError(t, err)
	assert.Equal(t, "failed", loaded.Status)
	require.Error(t, loaded.Error)
	assert.Contains(t, loaded.Error.Error(), "insufficient data")
}

func TestDiskStateStore_OverwriteExisting(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// Save initial state.
	snap := sampleSnapshot("bt-overwrite")
	snap.Status = "running"
	err := store.Save(ctx, snap)
	require.NoError(t, err)

	// Overwrite with completed state.
	snap.Status = "completed"
	snap.Frozen = true
	err = store.Save(ctx, snap)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "bt-overwrite")
	require.NoError(t, err)
	assert.Equal(t, "completed", loaded.Status)
	assert.True(t, loaded.Frozen)
}

// ─── Load error cases ───────────────────────────────────────────

func TestDiskStateStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	_, err := store.Load(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, ErrStateNotFound)
}

func TestDiskStateStore_LoadCorruptedFile(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// Write invalid JSON directly.
	path := store.pathFor("corrupt")
	err := os.WriteFile(path, []byte("{invalid json}"), 0o644)
	require.NoError(t, err)

	_, err = store.Load(ctx, "corrupt")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrStateNotFound)
}

func TestDiskStateStore_LoadEmptyFile(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	path := store.pathFor("empty")
	err := os.WriteFile(path, []byte(""), 0o644)
	require.NoError(t, err)

	_, err = store.Load(ctx, "empty")
	assert.Error(t, err)
}

// ─── Delete tests ───────────────────────────────────────────────

func TestDiskStateStore_Delete(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	snap := sampleSnapshot("bt-delete")
	err := store.Save(ctx, snap)
	require.NoError(t, err)

	err = store.Delete(ctx, "bt-delete")
	require.NoError(t, err)

	_, err = store.Load(ctx, "bt-delete")
	assert.ErrorIs(t, err, ErrStateNotFound)
}

func TestDiskStateStore_DeleteNonExistent(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	err := store.Delete(context.Background(), "nonexistent")
	require.NoError(t, err) // no-op
}

// ─── List / Exists tests ────────────────────────────────────────

func TestDiskStateStore_List(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		snap := sampleSnapshot("bt-list-" + string(rune('0'+i)))
		err := store.Save(ctx, snap)
		require.NoError(t, err)
	}

	ids, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, ids, 5)
}

func TestDiskStateStore_ListEmpty(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ids, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestDiskStateStore_Exists(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	exists, err := store.Exists(ctx, "bt-exists")
	require.NoError(t, err)
	assert.False(t, exists)

	err = store.Save(ctx, sampleSnapshot("bt-exists"))
	require.NoError(t, err)

	exists, err = store.Exists(ctx, "bt-exists")
	require.NoError(t, err)
	assert.True(t, exists)
}

// ─── Invalid ID tests ───────────────────────────────────────────

func TestDiskStateStore_SaveInvalidID(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	invalidIDs := []string{"", "a/b", "a\\b", "..", "a..b"}
	for _, id := range invalidIDs {
		err := store.Save(ctx, BacktestStateSnapshot{ID: id})
		assert.ErrorIs(t, err, ErrInvalidStateID, "ID %q should be invalid", id)
	}
}

func TestDiskStateStore_LoadInvalidID(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	_, err := store.Load(context.Background(), "../etc/passwd")
	assert.ErrorIs(t, err, ErrInvalidStateID)
}

// ─── Concurrent access tests ────────────────────────────────────

func TestDiskStateStore_ConcurrentSaveLoad(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup

	// Concurrent saves.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "bt-concurrent-" + string(rune('0'+i))
			err := store.Save(ctx, sampleSnapshot(id))
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Concurrent loads.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "bt-concurrent-" + string(rune('0'+i))
			_, err := store.Load(ctx, id)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all saved.
	ids, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, ids, n)
}

func TestDiskStateStore_ConcurrentSaveSameID(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap := sampleSnapshot("bt-same")
			snap.Status = "running"
			_ = store.Save(ctx, snap) // last writer wins
		}(i)
	}
	wg.Wait()

	loaded, err := store.Load(ctx, "bt-same")
	require.NoError(t, err)
	assert.Equal(t, "bt-same", loaded.ID)
}

// ─── Context cancellation tests ─────────────────────────────────

func TestDiskStateStore_SaveCancelledContext(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Save(ctx, sampleSnapshot("bt-cancelled"))
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDiskStateStore_LoadCancelledContext(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Load(ctx, "anything")
	assert.ErrorIs(t, err, context.Canceled)
}

// ─── Atomic write verification ──────────────────────────────────

func TestDiskStateStore_AtomicWrite(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	err := store.Save(ctx, sampleSnapshot("bt-atomic"))
	require.NoError(t, err)

	// Verify no .tmp file is left behind.
	entries, err := os.ReadDir(store.Dir())
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, filepath.Ext(e.Name()) == ".tmp",
			"temp file should not exist: %s", e.Name())
	}
}

// ─── JSON format verification ───────────────────────────────────

func TestDiskStateStore_JSONFormat(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	snap := sampleSnapshot("bt-json")
	err := store.Save(ctx, snap)
	require.NoError(t, err)

	path := store.pathFor("bt-json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Verify it's valid JSON with expected fields.
	var raw map[string]json.RawMessage
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Contains(t, raw, "id")
	assert.Contains(t, raw, "status")
	assert.Contains(t, raw, "params")
	assert.Contains(t, raw, "started_at")
	assert.Contains(t, raw, "completed_at")
	assert.Contains(t, raw, "frozen")
}
