package margin

import (
	"sync"
	"time"
)

// ============================================================
// ShortableList — 融券标的注册表
// ============================================================

// ShortableEntry 描述一只可融券标的的限制。
type ShortableEntry struct {
	Symbol  string    `json:"symbol"`
	MaxQty  float64   `json:"max_qty"` // 单券最大可融数量 (股); 0 = 无限制
	AddedAt time.Time `json:"added_at"`
}

// ShortableList 维护 symbol → ShortableEntry 映射, 支持线程安全
// 的查询 / 添加 / 删除。
//
// 内存数据, 不持久化: 融券标的名单由交易所每日公布, 启动时由外部
// (load-on-startup) 灌入即可。
type ShortableList struct {
	mu      sync.RWMutex
	entries map[string]ShortableEntry
}

// NewShortableList creates an empty shortable list.
func NewShortableList() *ShortableList {
	return &ShortableList{
		entries: make(map[string]ShortableEntry),
	}
}

// Add registers a symbol as shortable with the given max quantity.
// A maxQty of 0 means unlimited. If the symbol already exists, it is
// overwritten.
func (s *ShortableList) Add(symbol string, maxQty float64, addedAt time.Time) {
	if symbol == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[symbol] = ShortableEntry{
		Symbol:  symbol,
		MaxQty:  maxQty,
		AddedAt: addedAt,
	}
}

// Remove removes a symbol from the shortable list. No-op if not present.
func (s *ShortableList) Remove(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, symbol)
}

// IsShortable reports whether the symbol is registered as shortable.
func (s *ShortableList) IsShortable(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.entries[symbol]
	return ok
}

// Entry returns the shortable entry for a symbol. Returns false if
// the symbol is not registered.
func (s *ShortableList) Entry(symbol string) (ShortableEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[symbol]
	return e, ok
}

// MaxShortableQty returns the maximum shortable quantity for a symbol.
// Returns (-1, false) if the symbol is not shortable. Returns (0, true)
// if shortable with no limit (MaxQty == 0).
func (s *ShortableList) MaxShortableQty(symbol string) (float64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[symbol]
	if !ok {
		return -1, false
	}
	return e.MaxQty, true
}

// All returns a snapshot of all shortable entries sorted by symbol.
func (s *ShortableList) All() []ShortableEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ShortableEntry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out
}

// Count returns the number of registered shortable symbols.
func (s *ShortableList) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
