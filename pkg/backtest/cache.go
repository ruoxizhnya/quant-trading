package backtest

// P1-16 (Sprint 6, ODR-013 CQ-001, ADR-020):
// CacheManager — L1 OHLCV 缓存子组件，从 Engine God Object 抽离。
//
// 该文件从 pkg/backtest/engine.go (1446 行 God Object) 中抽离出
// 以下职责：
//
//   - 持有 inMemoryOHLCV map + atomic.Pointer 快照发布
//   - 提供 warmCache (bulk fetch from provider)
//   - 提供 LoadOHLCVInMemory (测试/bench 直接注入)
//   - 提供 getOHLCV (lock-free 读取 + binary-search 范围裁剪)
//   - 提供 dateRangeBounds (helper)
//
// Engine 通过 Engine.CacheManager() 访问；旧字段 e.inMemoryOHLCV 与
// 旧方法 (LoadOHLCVInMemory / getOHLCV / warmCache) 保留为
// backward-compat shim (6 个月)。
//
// 设计动机：
//   - 缓存管理是横切关注点，与回测编排逻辑无关
//   - 独立单测：构造 CacheManager + 假 Provider 即可验证 warm / hit / miss
//   - 未来扩展 (multi-tier cache, LRU eviction) 只动本文件
//
// 关键不变式：
//   - 调用方在 warmCache 或 LoadOHLCV 后**只读不写**快照
//   - atomic.Pointer 保证 hot path 读取零锁
//   - dateRangeBounds 依赖 bars 按日期升序排序（warmCache 强制排序）

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
)

// CacheManager 管理 L1 OHLCV 缓存。
//
// 缓存是 "per-backtest-lifecycle" 的——warm 一次，跑一次回测。
// 跨回测需调用 LoadOHLCVInMemory(nil) 清理或 LoadOHLCVInMemory(newData)
// 替换。
//
// 并发模型：
//   - 写入路径 (warm / Load) 持 mu.Lock
//   - 读取路径 (getOHLCV) lock-free via atomic.Pointer.Load
//   - 初始化时 Publish 一个空 map snapshot，保证 hot path 永远拿非 nil 指针
type CacheManager struct {
	mu sync.RWMutex

	// inMemoryOHLCV 主存储：symbol → bars (按日期升序)
	inMemoryOHLCV map[string][]domain.OHLCV

	// atomic 快照发布器。hot path 读取永远走 Load，不持 mu。
	inMemoryOHLCVAtomic atomic.Pointer[map[string][]domain.OHLCV]

	logger zerolog.Logger
}

// NewCacheManager 构造一个空 cache。
// 初始时 Publish 一个空 map snapshot，让 hot path 不会看到 nil。
func NewCacheManager(logger zerolog.Logger) *CacheManager {
	cm := &CacheManager{
		inMemoryOHLCV: make(map[string][]domain.OHLCV),
		logger:        logger.With().Str("component", "cache_manager").Logger(),
	}
	cm.inMemoryOHLCVAtomic.Store(&cm.inMemoryOHLCV)
	return cm
}

// Warm 预取 symbols 的全部 OHLCV 数据到 L1 缓存。
//
// 行为：
//   - 如果 L1 已包含所有 symbols → no-op（避免重复 bulk fetch）
//   - 否则从 provider 拉取，逐 symbol 按日期排序后写入
//   - 写入完成后 Publish 新快照到 atomic.Pointer
//
// Provider 通过 providerFn 注入（避免对 Engine 的直接依赖），便于
// 在没有 Engine 的场景下（如单测）独立测试 Warm。
func (cm *CacheManager) Warm(
	ctx context.Context,
	symbols []string,
	start, end time.Time,
	providerFn func() marketdata.Provider,
) error {
	if len(symbols) == 0 {
		return nil
	}

	// Fast path: L1 已含所有 symbols → 跳过 bulk fetch
	cm.mu.RLock()
	haveAll := len(cm.inMemoryOHLCV) >= len(symbols)
	if haveAll {
		for _, s := range symbols {
			if _, ok := cm.inMemoryOHLCV[s]; !ok {
				haveAll = false
				break
			}
		}
	}
	cm.mu.RUnlock()
	if haveAll {
		cm.logger.Debug().Int("symbols", len(symbols)).Msg("L1 OHLCV cache already warm — skipping bulk fetch")
		return nil
	}

	data, err := providerFn().BulkLoadOHLCV(ctx, symbols, start, end)
	if err != nil {
		return apperrors.Wrap(err, apperrors.ErrCodeUnavailable, "bulk OHLCV request failed", "CacheManager.Warm")
	}

	cm.mu.Lock()
	for symbol, bars := range data {
		// 强制按日期升序，确保 dateRangeBounds 的 binary-search 正确
		sort.Slice(bars, func(i, j int) bool {
			return bars[i].Date.Before(bars[j].Date)
		})
		cm.inMemoryOHLCV[symbol] = bars
	}
	// Publish 新快照供 lock-free 读
	cm.inMemoryOHLCVAtomic.Store(&cm.inMemoryOHLCV)
	cm.mu.Unlock()

	return nil
}

// Load 直接注入数据（测试 / 离线 bench 常用）。
// 传 nil 清理缓存（下次 Get 走 provider fallback）。
func (cm *CacheManager) Load(data map[string][]domain.OHLCV) {
	cm.mu.Lock()
	if data == nil {
		cm.inMemoryOHLCV = make(map[string][]domain.OHLCV)
	} else {
		cm.inMemoryOHLCV = data
	}
	cm.inMemoryOHLCVAtomic.Store(&cm.inMemoryOHLCV)
	cm.mu.Unlock()
}

// Get 读取单个 symbol 的 OHLCV 区间。
//
// 命中 L1 → zero-copy slice + binary-search 裁剪；
// 未命中 → 调用 providerFn 走 fallback 路径。
//
// 返回的 slice 与底层数组共享内存（仅 header 拷贝），调用方应
// 视其为只读。
func (cm *CacheManager) Get(
	ctx context.Context,
	symbol string,
	start, end time.Time,
	providerFn func() marketdata.Provider,
) ([]domain.OHLCV, error) {
	snap := cm.inMemoryOHLCVAtomic.Load()
	if snap != nil {
		if cached, ok := (*snap)[symbol]; ok {
			lo, hi := DateRangeBounds(cached, start, end)
			if lo > hi || lo >= len(cached) {
				return []domain.OHLCV{}, nil
			}
			if hi >= len(cached) {
				hi = len(cached) - 1
			}
			return cached[lo : hi+1], nil
		}
	}
	return providerFn().GetOHLCV(ctx, symbol, start, end)
}

// DateRangeBounds 返回 [lo, hi] 索引（bars 须按日期升序），使得
// bars[lo].Date 是第一个 >= start 的，bars[hi].Date 是最后一个 <= end 的。
// 无匹配时 lo > hi。
//
// 公开为 package-level 便于测试与外部 helper 使用。
func DateRangeBounds(bars []domain.OHLCV, start, end time.Time) (int, int) {
	n := len(bars)
	if n == 0 {
		return 0, -1
	}
	// Lower bound: first bar with Date >= start.
	lo := sort.Search(n, func(i int) bool {
		return !bars[i].Date.Before(start)
	})
	if lo == n {
		return n, n - 1 // no bar >= start
	}
	// Upper bound: last bar with Date <= end.
	hi := sort.Search(n, func(i int) bool {
		return bars[i].Date.After(end)
	})
	return lo, hi - 1
}
