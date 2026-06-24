package backtest

// P2-29 (TQ-016): Cross-day state persistence.
//
// The Engine's in-memory state (BacktestState) is lost on service
// restart. For long-running batch backtests that span multiple trading
// days, this means a restart loses all progress. DiskStateStore
// persists BacktestStateSnapshot values to disk as JSON, so a restart
// can reload the state and resume.
//
// Design:
//   - Each state is stored as {dir}/{id}.json.
//   - The error field is serialized as a string (error interface
//     cannot be JSON-marshalled directly).
//   - All operations are context-aware (honours cancellation).
//   - Concurrent access is safe (sync.RWMutex + atomic file writes).
//   - Writes are atomic: the file is written to a temp path and
//     renamed, so a crash mid-write never leaves a corrupt file.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// ErrStateNotFound is returned by Load when no persisted state exists
// for the given ID.
var ErrStateNotFound = errors.New("backtest: state not found")

// ErrInvalidStateID is returned when the ID contains path separators
// or other characters that would escape the store directory.
var ErrInvalidStateID = errors.New("backtest: invalid state ID")

// persistedState is the JSON-serializable form of BacktestStateSnapshot.
// The error field is converted to a string because the error interface
// cannot be JSON-marshalled.
type persistedState struct {
	ID          string                 `json:"id"`
	Status      string                 `json:"status"`
	Params      domain.BacktestParams  `json:"params"`
	Result      *domain.BacktestResult `json:"result,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	Frozen      bool                   `json:"frozen"`
}

// DiskStateStore persists BacktestStateSnapshot values to disk as JSON
// files. It is safe for concurrent use.
//
// File layout: {dir}/{id}.json
// Writes are atomic (temp file + rename).
type DiskStateStore struct {
	dir string
	mu  sync.RWMutex
}

// NewDiskStateStore creates a DiskStateStore rooted at dir. The
// directory is created if it doesn't exist.
func NewDiskStateStore(dir string) (*DiskStateStore, error) {
	if dir == "" {
		return nil, errors.New("backtest: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("backtest: create state dir: %w", err)
	}
	return &DiskStateStore{dir: dir}, nil
}

// Save persists the snapshot to {dir}/{id}.json. The write is atomic.
func (s *DiskStateStore) Save(ctx context.Context, snapshot BacktestStateSnapshot) error {
	if err := validateID(snapshot.ID); err != nil {
		return err
	}

	ps := toPersisted(snapshot)
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return fmt.Errorf("backtest: marshal state: %w", err)
	}

	path := s.pathFor(snapshot.ID)
	tmpPath := path + ".tmp"

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("backtest: write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("backtest: rename temp file: %w", err)
	}
	return nil
}

// Load reads and returns the persisted state for the given ID. Returns
// ErrStateNotFound if no file exists.
func (s *DiskStateStore) Load(ctx context.Context, id string) (BacktestStateSnapshot, error) {
	if err := validateID(id); err != nil {
		return BacktestStateSnapshot{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return BacktestStateSnapshot{}, err
	}

	path := s.pathFor(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return BacktestStateSnapshot{}, ErrStateNotFound
		}
		return BacktestStateSnapshot{}, fmt.Errorf("backtest: read state file: %w", err)
	}

	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return BacktestStateSnapshot{}, fmt.Errorf("backtest: unmarshal state: %w", err)
	}
	return fromPersisted(ps), nil
}

// Delete removes the persisted state for the given ID. No-op if absent.
func (s *DiskStateStore) Delete(ctx context.Context, id string) error {
	if err := validateID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	path := s.pathFor(id)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backtest: delete state file: %w", err)
	}
	return nil
}

// List returns the IDs of all persisted states.
func (s *DiskStateStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("backtest: list state dir: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		ids = append(ids, id)
	}
	return ids, nil
}

// Exists reports whether a persisted state exists for the given ID.
func (s *DiskStateStore) Exists(ctx context.Context, id string) (bool, error) {
	if err := validateID(id); err != nil {
		return false, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return false, err
	}

	_, err := os.Stat(s.pathFor(id))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Dir returns the store's root directory.
func (s *DiskStateStore) Dir() string {
	return s.dir
}

// pathFor returns the file path for the given state ID.
func (s *DiskStateStore) pathFor(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// validateID rejects IDs that contain path separators or are empty.
func validateID(id string) error {
	if id == "" {
		return ErrInvalidStateID
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return ErrInvalidStateID
	}
	return nil
}

// toPersisted converts a BacktestStateSnapshot to its JSON form.
func toPersisted(snap BacktestStateSnapshot) persistedState {
	ps := persistedState{
		ID:          snap.ID,
		Status:      snap.Status,
		Params:      snap.Params,
		Result:      snap.Result,
		StartedAt:   snap.StartedAt,
		CompletedAt: snap.CompletedAt,
		Frozen:      snap.Frozen,
	}
	if snap.Error != nil {
		ps.ErrorMsg = snap.Error.Error()
	}
	return ps
}

// fromPersisted converts a JSON form back to a BacktestStateSnapshot.
func fromPersisted(ps persistedState) BacktestStateSnapshot {
	snap := BacktestStateSnapshot{
		ID:          ps.ID,
		Status:      ps.Status,
		Params:      ps.Params,
		Result:      ps.Result,
		StartedAt:   ps.StartedAt,
		CompletedAt: ps.CompletedAt,
		Frozen:      ps.Frozen,
	}
	if ps.ErrorMsg != "" {
		snap.Error = errors.New(ps.ErrorMsg)
	}
	return snap
}
