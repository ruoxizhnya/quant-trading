package cache

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
	//
	// 不变式：发布的必须是 **拷贝**，不能是 &cm.inMemoryOHLCV 本身。
	// 存同一地址等于没隔离 —— 写者在 mu.Lock 内改 map，读者 lock-free
	// 读的还是同一个 map，构成并发 map 读写（Go runtime 可能直接 fatal，
	// 不可 recover）。见 publishLocked。
	inMemoryOHLCVAtomic atomic.Pointer[map[string][]domain.OHLCV]

	// 已 warm 的日期区间。warmed=false 表示尚未 warm（或已被 Load(nil) 清空）。
	//
	// 存在的原因：fast-path 此前只判断"symbol 是否在缓存里"，完全不看日期
	// 范围，于是任何不同区间的请求都会命中 fast-path 而复用旧区间的数据。
	// walk-forward 的 train→test 与窗口之间都会踩到。
	warmStart time.Time
	warmEnd   time.Time
	warmed    bool

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

	// Fast path: 已 warm 的区间**覆盖** [start, end]，且全部 symbol 都在缓存里
	// → 跳过 bulk fetch。
	//
	// 注意：必须校验日期区间，不能只校验 symbol。只校验 symbol 会让不同区间的
	// 请求命中 fast-path 而拿到错位数据（walk-forward train→test / 窗口间）。
	cm.mu.RLock()
	covered := cm.coversLocked(symbols, start, end)
	cm.mu.RUnlock()
	if covered {
		cm.logger.Debug().Int("symbols", len(symbols)).
			Time("start", start).Time("end", end).
			Msg("L1 OHLCV cache already warm for this range — skipping bulk fetch")
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
		// 与已有 bars 合并（而不是整体替换），让缓存区间可以单调递增。
		// mergeBars 返回新 slice，不复用入参底层数组 —— 已发布的快照因此不受影响。
		if existing, ok := cm.inMemoryOHLCV[symbol]; ok && len(existing) > 0 {
			cm.inMemoryOHLCV[symbol] = mergeBars(existing, bars)
		} else {
			cm.inMemoryOHLCV[symbol] = bars
		}
	}
	// 扩展已覆盖范围到本次请求的并集边界
	if !cm.warmed {
		cm.warmStart, cm.warmEnd, cm.warmed = start, end, true
	} else {
		if start.Before(cm.warmStart) {
			cm.warmStart = start
		}
		if end.After(cm.warmEnd) {
			cm.warmEnd = end
		}
	}
	cm.publishLocked()
	cm.mu.Unlock()

	return nil
}

// coversLocked 判断 [start, end] 是否已被当前 warm 区间覆盖、且 symbols 全部命中。
// 调用方须持有 mu（读锁即可）。
func (cm *CacheManager) coversLocked(symbols []string, start, end time.Time) bool {
	if !cm.warmed {
		return false
	}
	if start.Before(cm.warmStart) || end.After(cm.warmEnd) {
		return false
	}
	for _, s := range symbols {
		if _, ok := cm.inMemoryOHLCV[s]; !ok {
			return false
		}
	}
	return true
}

// publishLocked 发布一份 **map 拷贝** 供 lock-free 读路径使用。
// 调用方须持有 mu.Lock。
func (cm *CacheManager) publishLocked() {
	snapshot := make(map[string][]domain.OHLCV, len(cm.inMemoryOHLCV))
	for k, v := range cm.inMemoryOHLCV {
		snapshot[k] = v
	}
	cm.inMemoryOHLCVAtomic.Store(&snapshot)
}

// mergeBars 归并两个按日期升序的 bars 切片并去重（同一天以 incoming 为准）。
// 返回新分配的 slice，绝不复用入参底层数组。
func mergeBars(existing, incoming []domain.OHLCV) []domain.OHLCV {
	if len(existing) == 0 {
		return incoming
	}
	if len(incoming) == 0 {
		return existing
	}
	out := make([]domain.OHLCV, 0, len(existing)+len(incoming))
	i, j := 0, 0
	for i < len(existing) && j < len(incoming) {
		switch {
		case existing[i].Date.Before(incoming[j].Date):
			out = append(out, existing[i])
			i++
		case incoming[j].Date.Before(existing[i].Date):
			out = append(out, incoming[j])
			j++
		default: // 同一天：以后拉到的为准
			out = append(out, incoming[j])
			i++
			j++
		}
	}
	out = append(out, existing[i:]...)
	out = append(out, incoming[j:]...)
	return out
}

// boundsOf 返回 data 中所有 bars 的日期下界与上界；data 为空时 ok=false。
func boundsOf(data map[string][]domain.OHLCV) (lo, hi time.Time, ok bool) {
	for _, bars := range data {
		for _, b := range bars {
			if !ok || b.Date.Before(lo) {
				lo = b.Date
			}
			if !ok || b.Date.After(hi) {
				hi = b.Date
			}
			ok = true
		}
	}
	return lo, hi, ok
}

// Load 直接注入数据（测试 / 离线 bench 常用）。
// 传 nil 清理缓存（下次 Get 走 provider fallback）。
func (cm *CacheManager) Load(data map[string][]domain.OHLCV) {
	cm.mu.Lock()
	if data == nil {
		cm.inMemoryOHLCV = make(map[string][]domain.OHLCV)
		cm.warmed = false
		cm.warmStart, cm.warmEnd = time.Time{}, time.Time{}
	} else {
		// 浅拷贝一层 map：后续 Warm 的合并会替换 map 里的值，不应写回调用方持有的 map。
		copied := make(map[string][]domain.OHLCV, len(data))
		for k, v := range data {
			copied[k] = v
		}
		cm.inMemoryOHLCV = copied
		// 以注入数据的实际日期边界作为 warm 区间，保持「已注入的不必再 fetch」语义；
		// 但请求区间超出注入边界时仍会 fetch 补充 —— 这正是 fast-path 修复要的行为。
		lo, hi, ok := boundsOf(copied)
		cm.warmed = ok
		if ok {
			cm.warmStart, cm.warmEnd = lo, hi
		} else {
			cm.warmStart, cm.warmEnd = time.Time{}, time.Time{}
		}
	}
	cm.publishLocked()
	cm.mu.Unlock()
}

// Peek returns the cached OHLCV bars for a symbol, or nil if the symbol
// is not in the L1 cache. Unlike Get, Peek does NOT fall back to the
// provider on miss — it only checks the in-memory atomic snapshot.
//
// Used by Engine.calculatePosition to read cached bars without
// triggering a provider call. Callers that need provider fallback
// should use Get instead.
func (cm *CacheManager) Peek(symbol string) []domain.OHLCV {
	snap := cm.inMemoryOHLCVAtomic.Load()
	if snap == nil {
		return nil
	}
	return (*snap)[symbol]
}

// Snapshot returns the current L1 OHLCV cache state as a map.
//
// Used by tests (notably TestEngine_LoadOHLCVInMemory) to verify cache
// contents without accessing the unexported inMemoryOHLCVAtomic field.
// Returns nil if the cache has never been initialized (which never
// happens in practice because NewCacheManager publishes an empty map).
//
// The returned map shares memory with the internal cache — callers
// must treat it as read-only.
func (cm *CacheManager) Snapshot() map[string][]domain.OHLCV {
	snap := cm.inMemoryOHLCVAtomic.Load()
	if snap == nil {
		return nil
	}
	return *snap
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
