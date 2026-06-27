// Package sentiment provides lightweight, storage-free aggregation of
// news, social, and analyst sentiment scores per stock per day.
//
// Callers Add individual SentimentScore values throughout the day and
// query daily averages (GetDailyAverage) or top-N rankings
// (GetTopPositive / GetTopNegative). All operations are safe for
// concurrent use.
//
// The aggregator keeps everything in memory; for durable storage wire
// a downstream consumer to drain the scores. Day matching uses the
// Date field's calendar day (in whatever time zone the caller supplied),
// so callers should normalize to a consistent location before Add.
package sentiment

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// SentimentScore represents a single sentiment signal for a symbol.
type SentimentScore struct {
	Symbol     string
	Date       time.Time
	Score      float64 // -1.0 (very negative) to 1.0 (very positive)
	Confidence float64 // 0.0 to 1.0
	Source     string  // "news", "social", "analyst"
	Headline   string
}

// SentimentAggregator collects and aggregates sentiment scores per
// stock per day. It is safe for concurrent use.
type SentimentAggregator struct {
	mu     sync.RWMutex
	scores map[string][]SentimentScore // symbol → scores
}

// NewSentimentAggregator creates a new aggregator.
func NewSentimentAggregator() *SentimentAggregator {
	return &SentimentAggregator{
		scores: make(map[string][]SentimentScore),
	}
}

// Add appends a sentiment score. The score is stored as-is (including
// its Date), enabling cross-day queries.
func (sa *SentimentAggregator) Add(score SentimentScore) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.scores[score.Symbol] = append(sa.scores[score.Symbol], score)
}

// sameDay reports whether a and b fall on the same calendar day.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// GetDailyAverage returns the simple (unweighted) average sentiment for
// a symbol on the given calendar day. Returns an error when the symbol
// is unknown or has no scores on the requested day.
func (sa *SentimentAggregator) GetDailyAverage(symbol string, date time.Time) (float64, error) {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	scores, ok := sa.scores[symbol]
	if !ok {
		return 0, fmt.Errorf("no sentiment scores for symbol %q", symbol)
	}
	var sum float64
	var n int
	for _, s := range scores {
		if !sameDay(s.Date, date) {
			continue
		}
		sum += s.Score
		n++
	}
	if n == 0 {
		return 0, fmt.Errorf("no sentiment scores for symbol %q on %s", symbol, date.Format("2006-01-02"))
	}
	return sum / float64(n), nil
}

// GetTopPositive returns the top N most positive stocks for a date,
// ranked by daily average sentiment descending. Results are capped to N
// and to symbols with scores on that date. Each returned SentimentScore
// carries the daily average in Score, the average confidence in
// Confidence, and "aggregated" in Source.
func (sa *SentimentAggregator) GetTopPositive(date time.Time, n int) []SentimentScore {
	return sa.topN(date, n, false)
}

// GetTopNegative returns the top N most negative stocks for a date,
// ranked by daily average sentiment ascending.
func (sa *SentimentAggregator) GetTopNegative(date time.Time, n int) []SentimentScore {
	return sa.topN(date, n, true)
}

// topN computes per-symbol daily averages for the given date and returns
// the top n. When ascending is true, the lowest averages come first
// (most negative); otherwise the highest come first (most positive).
func (sa *SentimentAggregator) topN(date time.Time, n int, ascending bool) []SentimentScore {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	type avg struct {
		symbol     string
		score      float64
		confidence float64
	}
	var avgs []avg
	for symbol, scores := range sa.scores {
		var sumScore, sumConf float64
		var count int
		for _, s := range scores {
			if !sameDay(s.Date, date) {
				continue
			}
			sumScore += s.Score
			sumConf += s.Confidence
			count++
		}
		if count == 0 {
			continue
		}
		avgs = append(avgs, avg{
			symbol:     symbol,
			score:      sumScore / float64(count),
			confidence: sumConf / float64(count),
		})
	}
	if ascending {
		sort.Slice(avgs, func(i, j int) bool { return avgs[i].score < avgs[j].score })
	} else {
		sort.Slice(avgs, func(i, j int) bool { return avgs[i].score > avgs[j].score })
	}
	if n < 0 {
		n = 0
	}
	if n > len(avgs) {
		n = len(avgs)
	}
	out := make([]SentimentScore, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, SentimentScore{
			Symbol:     avgs[i].symbol,
			Date:       date,
			Score:      avgs[i].score,
			Confidence: avgs[i].confidence,
			Source:     "aggregated",
		})
	}
	return out
}
