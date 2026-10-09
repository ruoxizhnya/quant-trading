package bootstrap

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/backtest"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/live"
	"github.com/ruoxizhnya/quant-trading/pkg/marketdata"
	"github.com/ruoxizhnya/quant-trading/pkg/risk"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// BuildBacktestEngine 构造回测引擎与 HTTP 行情 provider。HTTP provider
// 单独返回，供 DataAdapter 的多源回退路径复用。
func BuildBacktestEngine(v *viper.Viper, logger zerolog.Logger) (*backtest.Engine, marketdata.Provider) {
	dataServiceURL := v.GetString("data_service.url")
	if dataServiceURL == "" {
		dataServiceURL = "http://localhost:8081"
	}
	httpProvider := marketdata.NewHTTPProvider(dataServiceURL, logger)
	engine, err := backtest.NewEngine(v, httpProvider, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize backtest engine")
	}
	return engine, httpProvider
}

// BuildRiskManager 从 risk_manager.* 配置构造进程内风控管理器（P1-15，
// ODR-021 合并形态），同时作为 tools 门面 get_market_regime 的
// RegimeDetectorClient（ai-service 也需要它 —— 两处共享同一构造）。
func BuildRiskManager(v *viper.Viper, logger zerolog.Logger) *risk.RiskManager {
	riskCfg := risk.RiskManagerConfig{
		TargetVolatility:    v.GetFloat64("risk_manager.target_volatility"),
		MaxPositionWeight:   v.GetFloat64("risk_manager.max_position_weight"),
		MinPositionWeight:   v.GetFloat64("risk_manager.min_position_weight"),
		ATRPeriod:           v.GetInt("risk_manager.stoploss.atr_period"),
		BaseMultiplier:      v.GetFloat64("risk_manager.stoploss.base_multiplier"),
		BullMultiplier:      v.GetFloat64("risk_manager.stoploss.bull_multiplier"),
		BearMultiplier:      v.GetFloat64("risk_manager.stoploss.bear_multiplier"),
		SidewaysMultiplier:  v.GetFloat64("risk_manager.stoploss.sideways_multiplier"),
		TakeProfitMult:      v.GetFloat64("risk_manager.take_profit.atr_multiplier"),
		VolLookbackDays:     v.GetInt("risk_manager.volatility.lookback_days"),
		AnnualizationFactor: v.GetFloat64("risk_manager.volatility.annualization_factor"),
		FastMAPeriod:        v.GetInt("risk_manager.regime.fast_ma_period"),
		SlowMAPeriod:        v.GetInt("risk_manager.regime.slow_ma_period"),
		RegimeVolLookback:   v.GetInt("risk_manager.regime.vol_lookback"),
	}
	rm, err := risk.NewRiskManager(riskCfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize in-process risk manager (P1-15)")
	}
	logger.Info().Msg("risk manager attached to backtest engine in-process (P1-15)")
	return rm
}

// BuildExecutionTrader 构造进程内 MockTrader（P1-15, ODR-021）。
// analysis 与 ai 共享同一构造 —— 两边跑出来的回测必须逐项一致。
func BuildExecutionTrader(v *viper.Viper, logger zerolog.Logger) live.LiveTrader {
	execConfig := domain.ExecutionConfig{
		OrderType:      domain.OrderTypeMarket,
		SlippageModel:  "fixed",
		CommissionRate: v.GetFloat64("backtest.commission_rate"),
		MinCommission:  v.GetFloat64("trading.min_commission"),
		InitialCapital: v.GetFloat64("backtest.initial_capital"),
	}
	trader := live.NewMockTrader(live.MockTraderConfig{
		InitialCash:    execConfig.InitialCapital,
		CommissionRate: execConfig.CommissionRate,
		StampTaxRate:   v.GetFloat64("trading.stamp_tax_rate"),
		SlippageRate:   v.GetFloat64("backtest.slippage_rate"),
	}, logger)
	logger.Info().Msg("execution trader attached to backtest engine in-process (P1-15)")
	return trader
}

// InitStore 构造 Postgres store（DSN 组装与连接失败均 fail-loud）。
func InitStore(v *viper.Viper, logger zerolog.Logger) *storage.PostgresStore {
	dbURL, err := storage.BuildDSN(storage.DatabaseConfig{
		URL:      v.GetString("database.url"),
		Host:     v.GetString("database.host"),
		Port:     v.GetInt("database.port"),
		User:     v.GetString("database.user"),
		Password: v.GetString("database.password"),
		Name:     v.GetString("database.database"),
		SSLMode:  v.GetString("database.sslmode"),
	})
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid database configuration")
	}
	store, err := storage.NewPostgresStore(context.Background(), dbURL)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize postgres store")
	}
	return store
}
