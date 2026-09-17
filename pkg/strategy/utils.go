package strategy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func IsRebalanceDay(date time.Time, frequency string) bool {
	switch frequency {
	case "weekly":
		if date.Weekday() == time.Monday {
			return true
		}
		prevDay := date.AddDate(0, 0, -1)
		if prevDay.Weekday() == time.Sunday && date.Weekday() == time.Tuesday {
			return true
		}
		if prevDay.Weekday() == time.Saturday && date.Weekday() == time.Monday {
			return true
		}
		return false
	case "monthly":
		if date.Day() == 1 {
			return true
		}
		if date.Day() <= 3 {
			prevDay := date.AddDate(0, 0, -1)
			if prevDay.Month() != date.Month() {
				return true
			}
		}
		return false
	case "daily", "":
		return true
	default:
		return true
	}
}

// LatestBarDate 从行情里取最新的一根 bar 的日期。
//
// 它是判断「今天」最可靠的来源：日期来自被回放的数据本身，而不是墙钟。
// 回测里任何依赖墙钟的判断都会让结果随运行日期漂移 —— weekly 周一跑有
// 信号、周四跑零成交（P1-12）。取不到日期返回零值，调用方应据此拒绝
// 生成信号，而不是退回 time.Now()。
func LatestBarDate(bars map[string][]domain.OHLCV) time.Time {
	var latest time.Time
	for _, series := range bars {
		if len(series) == 0 {
			continue
		}
		if d := series[len(series)-1].Date; d.After(latest) {
			latest = d
		}
	}
	return latest
}

type ScreenCache struct {
	mu    sync.Mutex
	store map[string][]domain.ScreenResult
	order []string // insertion order (oldest first); used for deterministic eviction
	limit int
}

func NewScreenCache(limit int) *ScreenCache {
	if limit < 0 {
		limit = 0
	}
	return &ScreenCache{
		store: make(map[string][]domain.ScreenResult),
		order: make([]string, 0),
		limit: limit,
	}
}

func (c *ScreenCache) Get(key string) ([]domain.ScreenResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.store[key]
	return v, ok
}

func (c *ScreenCache) Set(key string, value []domain.ScreenResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, remove its old position in order slice.
	if _, exists := c.store[key]; exists {
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
	}

	c.store[key] = value
	c.order = append(c.order, key)

	// Evict oldest entries until we are at or under the limit.
	// limit=0 means "never store anything" — evict immediately.
	for len(c.store) > c.limit {
		if len(c.order) == 0 {
			// Defensive: should not happen, but prevent infinite loop.
			break
		}
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.store, oldest)
	}
}

func CallScreenAPI(client *http.Client, dataServiceURL string, cache *ScreenCache, req domain.ScreenRequest) ([]domain.ScreenResult, error) {
	if cached, ok := cache.Get(req.Date); ok {
		return cached, nil
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal screen request: %w", err)
	}

	url := dataServiceURL + "/screen"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create screen request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq.WithContext(context.Background()))
	if err != nil {
		return nil, fmt.Errorf("failed to call screen API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("screen API returned status %d", resp.StatusCode)
	}

	var result struct {
		Count   int                   `json:"count"`
		Results []domain.ScreenResult `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode screen response: %w", err)
	}

	cache.Set(req.Date, result.Results)
	return result.Results, nil
}
