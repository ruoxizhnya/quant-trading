package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/ruoxizhnya/quant-trading/pkg/httpclient"
	"github.com/rs/zerolog"
	"os"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
	
	// Create engine
	cfg := backtest.Config{
		InitialCapital: 1000000,
		CommissionRate: 0.0003,
		SlippageRate:   0.0001,
	}
	
	store, err := storage.NewPostgresStore(context.Background(), "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable")
	if err != nil {
		fmt.Println("Store error:", err)
		return
	}
	defer store.Close()
	
	engine, err := backtest.NewEngine(cfg, store, "http://localhost:8083", httpclient.NewClient(30*time.Second), logger)
	if err != nil {
		fmt.Println("Engine error:", err)
		return
	}
	
	req := backtest.BacktestRequest{
		Strategy:       "multi_factor",
		StockPool:      []string{"600000.SH"},
		StartDate:      "2024-01-01",
		EndDate:        "2024-06-01",
		InitialCapital: 1000000,
	}
	
	result, err := engine.RunBacktest(context.Background(), req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Total Trades: %d\n", result.TotalTrades)
	fmt.Printf("Total Return: %.4f\n", result.TotalReturn)
	fmt.Printf("Final Value: %.2f\n", result.FinalValue)
}
