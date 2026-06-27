package sentiment

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// day parses a YYYY-MM-DD string into a time.Time for test fixtures.
func day(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse("2006-01-02", s)
	require.NoError(t, err)
	return tt
}

func TestSentimentAggregator_Add(t *testing.T) {
	sa := NewSentimentAggregator()
	d := day(t, "2026-06-01")
	sa.Add(SentimentScore{Symbol: "AAPL", Date: d, Score: 0.5, Confidence: 0.9, Source: "news", Headline: "good"})
	sa.Add(SentimentScore{Symbol: "AAPL", Date: d, Score: -0.2, Confidence: 0.8, Source: "social"})

	avg, err := sa.GetDailyAverage("AAPL", d)
	require.NoError(t, err)
	assert.InDelta(t, (0.5-0.2)/2, avg, 1e-9)

	// Unknown symbol returns an error.
	_, err = sa.GetDailyAverage("MISSING", d)
	require.Error(t, err)
}

func TestSentimentAggregator_GetDailyAverage(t *testing.T) {
	sa := NewSentimentAggregator()
	d1 := day(t, "2026-06-01")
	d2 := day(t, "2026-06-02")
	sa.Add(SentimentScore{Symbol: "MSFT", Date: d1, Score: 0.8, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "MSFT", Date: d1, Score: 0.2, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "MSFT", Date: d2, Score: -0.5, Confidence: 1.0})

	avg, err := sa.GetDailyAverage("MSFT", d1)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, avg, 1e-9)

	avg, err = sa.GetDailyAverage("MSFT", d2)
	require.NoError(t, err)
	assert.InDelta(t, -0.5, avg, 1e-9)

	// No scores on a different day → error.
	_, err = sa.GetDailyAverage("MSFT", day(t, "2026-06-03"))
	require.Error(t, err)
}

func TestSentimentAggregator_GetTopPositive(t *testing.T) {
	sa := NewSentimentAggregator()
	d := day(t, "2026-06-01")
	sa.Add(SentimentScore{Symbol: "A", Date: d, Score: 0.3, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "B", Date: d, Score: 0.9, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "C", Date: d, Score: -0.5, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "D", Date: d, Score: 0.6, Confidence: 1.0})

	top := sa.GetTopPositive(d, 2)
	require.Len(t, top, 2)
	assert.Equal(t, "B", top[0].Symbol)
	assert.InDelta(t, 0.9, top[0].Score, 1e-9)
	assert.Equal(t, "D", top[1].Symbol)
	assert.InDelta(t, 0.6, top[1].Score, 1e-9)
	assert.Equal(t, "aggregated", top[0].Source)

	// n larger than available returns all, ordered descending.
	all := sa.GetTopPositive(d, 10)
	require.Len(t, all, 4)
	assert.Equal(t, "B", all[0].Symbol)
	assert.Equal(t, "C", all[3].Symbol)

	// Scores on other days are excluded.
	sa.Add(SentimentScore{Symbol: "E", Date: day(t, "2026-06-02"), Score: 1.0, Confidence: 1.0})
	require.Len(t, sa.GetTopPositive(d, 10), 4)
}

func TestSentimentAggregator_GetTopNegative(t *testing.T) {
	sa := NewSentimentAggregator()
	d := day(t, "2026-06-01")
	sa.Add(SentimentScore{Symbol: "A", Date: d, Score: 0.3, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "B", Date: d, Score: -0.9, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "C", Date: d, Score: -0.1, Confidence: 1.0})
	sa.Add(SentimentScore{Symbol: "D", Date: d, Score: 0.6, Confidence: 1.0})

	top := sa.GetTopNegative(d, 2)
	require.Len(t, top, 2)
	assert.Equal(t, "B", top[0].Symbol)
	assert.InDelta(t, -0.9, top[0].Score, 1e-9)
	assert.Equal(t, "C", top[1].Symbol)
	assert.InDelta(t, -0.1, top[1].Score, 1e-9)
}

func TestSentimentAggregator_Empty(t *testing.T) {
	sa := NewSentimentAggregator()
	d := day(t, "2026-06-01")

	_, err := sa.GetDailyAverage("NOPE", d)
	require.Error(t, err)

	assert.Empty(t, sa.GetTopPositive(d, 5))
	assert.Empty(t, sa.GetTopNegative(d, 5))
	// Negative n returns empty.
	assert.Empty(t, sa.GetTopPositive(d, -1))
}
