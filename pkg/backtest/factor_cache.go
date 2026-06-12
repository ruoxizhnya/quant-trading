package backtest

// P1-16 (Sprint 6, ODR-013 CQ-001, ADR-020):
// FactorCacheAccessor — L1 因子 z-score 缓存子组件，从 Engine 抽离。
//
// 该文件从 pkg/backtest/engine.go 中抽离出以下职责：
//
//   - 持有 factorCache (factorType → date → symbol → z-score)
//   - 提供 LoadFactorCache (直接注入)
//   - 提供 GetFactorZScore (lock-free 读取)
//   - 提供 Warm (从 storage.PostgresStore 批量预热)
//
// Engine 通过 Engine.FactorCache() 访问；旧字段 e.factorCache 与
// 旧方法 (LoadFactorCache / GetFactorZScore / warmFactorCache) 保留为
// backward-compat shim (6 个月)。
//
// 设计动机：
//   - 因子缓存与 OHLCV 缓存是**两套独立关注点**：
//     * OHLCV 缓存由 CacheManager 持有（L1 volatile）
//     * 因子缓存由 FactorCacheAccessor 持有（可由 DB 持久层 warm）
//   - 拆分后两个组件可独立单测、独立演进
//   - future: 因子缓存可引入 lazy load / TTL / 持久化而不影响 OHLCV 路径

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// FactorStore is the minimal interface FactorCacheAccessor needs from a
// persistence layer. *storage.PostgresStore satisfies this.
type FactorStore interface {
	GetFactorCacheRange(ctx context.Context, factor domain.FactorType, start, end time.Time) ([]*domain.FactorCacheEntry, error)
}

// FactorCacheAccessor manages the L1 factor z-score cache.
type FactorCacheAccessor struct {
	mu    sync.RWMutex
	cache map[domain.FactorType]map[time.Time]map[string]float64

	logger zerolog.Logger
}

// NewFactorCacheAccessor 构造一个空 factor cache accessor。
func NewFactorCacheAccessor(logger zerolog.Logger) *FactorCacheAccessor {
	return &FactorCacheAccessor{
		logger: logger.With().Str("component", "factor_cache").Logger(),
	}
}

// Load 直接注入数据（典型用法：storage.GetFactorCacheRange 后的结果）。
// 传 nil 清理缓存。
func (f *FactorCacheAccessor) Load(data map[domain.FactorType]map[time.Time]map[string]float64) {
	f.mu.Lock()
	f.cache = data
	f.mu.Unlock()
}

// Get 返回 (z-score, exists)。无命中返回 (0, false)。
// 读路径持 RLock（与 OHLCV cache 的 lock-free 不同，因为命中频率低
// 且 map 体积通常远小于 OHLCV 缓存）。
func (f *FactorCacheAccessor) Get(factor domain.FactorType, date time.Time, symbol string) (float64, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.cache == nil {
		return 0, false
	}
	dateMap, ok := f.cache[factor]
	if !ok {
		return 0, false
	}
	symbolMap, ok := dateMap[date]
	if !ok {
		return 0, false
	}
	z, ok := symbolMap[symbol]
	return z, ok
}

// Warm 从 store 批量预热 3 类常用因子（momentum/value/quality）。
// store 为 nil 时 no-op；具体因子加载失败时打 Warn 不中断。
//
// Nil-check 注意事项：FactorStore 是 interface。Go 的 typed-nil 陷阱
// 意味着传入 (*storage.PostgresStore)(nil) 会得到一个**非 nil 的
// interface 值**——直接调用方法会 panic。调用方（特别是
// Engine.warmFactorCache）需要在传入前自行判空 underlying pointer。
// 这里只做 interface==nil 检查。
func (f *FactorCacheAccessor) Warm(ctx context.Context, start, end time.Time, store FactorStore) error {
	if store == nil {
		return nil
	}

	factors := []domain.FactorType{domain.FactorMomentum, domain.FactorValue, domain.FactorQuality}
	combined := make(map[domain.FactorType]map[time.Time]map[string]float64)

	for _, factor := range factors {
		entries, err := store.GetFactorCacheRange(ctx, factor, start, end)
		if err != nil {
			f.logger.Warn().Str("factor", string(factor)).Err(err).Msg("Failed to load factor cache from DB")
			continue
		}
		if len(entries) == 0 {
			f.logger.Info().Str("factor", string(factor)).Msg("No factor cache entries in DB — run sync/factors/all first")
			continue
		}
		for _, entry := range entries {
			if entry == nil {
				continue
			}
			if combined[entry.FactorName] == nil {
				combined[entry.FactorName] = make(map[time.Time]map[string]float64)
			}
			if combined[entry.FactorName][entry.TradeDate] == nil {
				combined[entry.FactorName][entry.TradeDate] = make(map[string]float64)
			}
			combined[entry.FactorName][entry.TradeDate][entry.Symbol] = entry.ZScore
		}
		f.logger.Info().
			Str("factor", string(factor)).
			Int("entries", len(entries)).
			Msg("Factor cache loaded from DB")
	}

	if len(combined) > 0 {
		f.mu.Lock()
		f.cache = combined
		f.mu.Unlock()
	}

	return nil
}

// Len 返回当前缓存中的 factor 数量（仅供测试 / 监控）。
func (f *FactorCacheAccessor) Len() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.cache)
}

// Compile-time check that *storage.PostgresStore satisfies FactorStore.
var _ FactorStore = (*storage.PostgresStore)(nil)
