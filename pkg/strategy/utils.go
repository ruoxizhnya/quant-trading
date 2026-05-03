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

type ScreenCache struct {
	mu    sync.Mutex
	store map[string][]domain.ScreenResult
	limit int
}

func NewScreenCache(limit int) *ScreenCache {
	return &ScreenCache{store: make(map[string][]domain.ScreenResult), limit: limit}
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
	c.store[key] = value
	if len(c.store) > c.limit {
		for k := range c.store {
			delete(c.store, k)
			break
		}
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
