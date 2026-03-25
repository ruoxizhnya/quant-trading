// +build ignore

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

func main() {
	ctx := context.Background()

	store, err := storage.NewPostgresStore(ctx,
		"postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer store.Close()

	// Use Redis as cache
	cache, err := storage.NewCache("redis://localhost:6379/0")
	if err != nil {
		panic(err)
	}
	defer cache.Close()

	dc := data.NewDataCache(cache, store)

	// Get 50 symbols from OHLCV table
	rows, err := store.DB().Query(ctx,
		`SELECT DISTINCT symbol FROM ohlcv_daily_qfq WHERE symbol LIKE '60%' LIMIT 50`)
	if err != nil {
		panic(err)
	}
	var symbols []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		symbols = append(symbols, s)
	}
	rows.Close()

	fmt.Printf("Testing %d symbols × 1 year\n", len(symbols))

	// Test 1 goroutine (sequential baseline)
	fmt.Println("\n--- Sequential (1 worker) ---")
	start1 := time.Now()
	err = dc.WarmCacheWithWorkers(ctx, symbols, "2023-01-01", "2023-12-31", 1)
	seq := time.Since(start1)
	fmt.Printf("Time: %v\n", seq)
	fmt.Printf("Per symbol: %v\n", seq/time.Duration(len(symbols)))

	// Test 8 goroutines (parallel)
	fmt.Println("\n--- Parallel (8 workers) ---")
	start2 := time.Now()
	err = dc.WarmCacheWithWorkers(ctx, symbols, "2023-01-01", "2023-12-31", 8)
	par := time.Since(start2)
	fmt.Printf("Time: %v\n", par)
	fmt.Printf("Per symbol: %v\n", par/time.Duration(len(symbols)))

	speedup := float64(seq) / float64(par)
	fmt.Printf("\nSpeedup: %.1fx\n", speedup)

	fmt.Printf("\nExtrapolated 500 symbols × 1 year (sequential): %v\n", (seq/time.Duration(len(symbols)))*500)
	fmt.Printf("Extrapolated 500 symbols × 1 year (8 workers): %v\n", (par/time.Duration(len(symbols)))*500)
}
