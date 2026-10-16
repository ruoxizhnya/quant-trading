package source

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// toStoragePoints projects the in-memory UnifiedDataPoint to the
// storage layer's struct. Kept local to avoid an import cycle on
// the storage package.
func toStoragePoints(in []UnifiedDataPoint) []storage.UnifiedDataPoint {
	out := make([]storage.UnifiedDataPoint, len(in))
	for i, p := range in {
		out[i] = storage.UnifiedDataPoint{
			Symbol:     p.Symbol,
			TradeTime:  p.TradeTime,
			Source:     p.Source,
			DataType:   p.DataType,
			Data:       p.Data,
			IngestTime: p.IngestTime,
		}
	}
	return out
}

// ETLPipeline is the high-level "fetch → normalize → validate → dedup → persist"
// coordinator. It is the single integration point between the Registry
// and the storage layer.
//
// Why a pipeline type? Centralizing the flow here keeps the per-adapter
// code focused on normalization; the pipeline handles cross-cutting
// concerns (logging, metrics, retry policy).
//
// DATA-08（2026-10-16，主任裁决「停用路线」）：本类型今天**零生产调用
// 方** —— 生产数据同步全部直连 pkg/data.TushareClient（handlers_sync.go /
// sync_handlers.go），不过 Registry。因此 Registry.Fetch 的 fallback 链
// 实际不可达；链路保持实现与测试覆盖（零件先写、后接线），接线前置
// 条件（源单位实测 → 同步边界换算 → Normalizer → 与 sync_jobs 的关系）
// 登记在 DATA-10。该状态由 internal/repoguard/registry_etl_wiring_test.go
// 机械钉住：生产代码出现 NewETLPipeline 调用即红。
type ETLPipeline struct {
	Registry *Registry
	// CR-21 (ODR-012): Store is now `storage.BulkInserter` (the interface
	// in pkg/storage), not `*storage.PostgresStore`. This lets tests inject
	// a stub that satisfies the real signature (`[]storage.UnifiedDataPoint`)
	// and forces every caller to match the production code path.
	Store storage.BulkInserter
}

// NewETLPipeline constructs a pipeline. Either field may be nil for tests.
//
// DATA-08（2026-10-16）：生产侧零调用方（裁决「停用路线」）—— 今天的
// 构造点只有 etl_test.go / integration_test.go。接线见 DATA-10。
func NewETLPipeline(reg *Registry, store storage.BulkInserter) *ETLPipeline {
	return &ETLPipeline{Registry: reg, Store: store}
}

// ProcessResult summarizes one ETL run.
type ProcessResult struct {
	DataType   string
	Source     string
	Fetched    int           // points returned by adapter
	Persisted  int           // points successfully written
	Skipped    int           // dropped by validate / dedup
	Duration   time.Duration // wall-clock time
	SourceName string        // adapter name that served the data
}

// Process executes the full ETL flow for req.
//
// Steps:
//
//  1. Fetch via the Registry (handles fallback chain transparently).
//  2. Normalize FetchResponse.Items → []UnifiedDataPoint.
//  3. Validate (lightweight field checks).
//  4. Deduplicate.
//  5. Persist via Store.BulkInsert. The insert is a real implementation
//     (pkg/storage/bulk_insert.go: single transaction, per-row
//     ON CONFLICT upsert, table selected by DataType) — an earlier
//     version of this comment called it "a stub; Sprint 1 wires the
//     real insert", which went stale when ODR-011 landed the real
//     BulkInsert (dfec481). 叙述与实现分叉正是本项目反复登记的缺陷
//     家族，故此处按实现如实描述。
//
// Persistence is delegated to the storage layer; the pipeline does not
// know SQL. Adapters only need to return FetchResponse; storage knows
// the table layout.
func (p *ETLPipeline) Process(ctx context.Context, req FetchRequest, normalizer Normalizer) (*ProcessResult, error) {
	start := time.Now()
	if p.Registry == nil {
		return nil, fmt.Errorf("etl: nil registry")
	}
	if normalizer == nil {
		return nil, fmt.Errorf("etl: nil normalizer for %s", req.DataType)
	}

	// 1. Fetch
	resp, err := p.Registry.Fetch(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("etl: fetch %s: %w", req.DataType, err)
	}

	// 2. Normalize
	points := make([]UnifiedDataPoint, 0, len(resp.Items))
	for _, item := range resp.Items {
		dp := normalizer(item, resp.Source, req.DataType)
		if dp.Data == nil {
			continue // normalizer signals "skip" via nil Data
		}
		dp.IngestTime = time.Now().UTC()
		points = append(points, dp)
	}

	// 3. Validate (lightweight; per-data-type validators can be added later)
	points, validateSkipped := ValidatePoints(points)

	// 4. Deduplicate
	points, dedupSkipped := DeduplicateWithCount(points)
	skipped := validateSkipped + dedupSkipped

	// 5. Persist (delegated). The storage layer's BulkInsert* methods
	// accept UnifiedDataPoint values; the table is selected by DataType.
	persisted := 0
	persistSkipped := 0
	if p.Store != nil {
		n, sk, err := p.Store.BulkInsert(ctx, req.DataType, toStoragePoints(points))
		if err != nil {
			return &ProcessResult{
				DataType:   req.DataType,
				SourceName: resp.Source,
				Fetched:    len(resp.Items),
				Persisted:  0,
				Skipped:    skipped + sk,
				Duration:   time.Since(start),
			}, fmt.Errorf("etl: persist %s: %w", req.DataType, err)
		}
		persisted = n
		persistSkipped = sk
	}

	return &ProcessResult{
		DataType:   req.DataType,
		Source:     resp.Source,
		SourceName: resp.Source,
		Fetched:    len(resp.Items),
		Persisted:  persisted,
		Skipped:    skipped + persistSkipped,
		Duration:   time.Since(start),
	}, nil
}

// Normalizer converts a raw DataItem (from an adapter) into a UnifiedDataPoint.
// Returning a point with Data==nil signals "drop this item".
type Normalizer func(item DataItem, source, dataType string) UnifiedDataPoint

// ValidatePoints performs lightweight sanity checks:
//   - Symbol must be non-empty
//   - TradeTime must not be zero
//
// Returns the surviving points and the number dropped.
func ValidatePoints(points []UnifiedDataPoint) ([]UnifiedDataPoint, int) {
	skipped := 0
	out := make([]UnifiedDataPoint, 0, len(points))
	for _, p := range points {
		if p.Symbol == "" {
			skipped++
			continue
		}
		if p.TradeTime.IsZero() {
			skipped++
			continue
		}
		if p.Data == nil {
			skipped++
			continue
		}
		out = append(out, p)
	}
	return out, skipped
}
