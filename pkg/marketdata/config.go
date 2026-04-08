package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

type DataSourceConfig struct {
	Primary  string            `mapstructure:"primary" json:"primary"`
	Fallback string            `mapstructure:"fallback" json:"fallback"`
	Cache    *CacheConfig      `mapstructure:"cache" json:"cache,omitempty"`
	Sources  map[string]SourceConfig `mapstructure:"sources" json:"sources"`
}

type SourceConfig struct {
	Type     string            `mapstructure:"type" json:"type"`
	URL      string            `mapstructure:"url" json:"url,omitempty"`
	Token    string            `mapstructure:"token" json:"token,omitempty"`
	DBURL    string            `mapstructure:"db_url" json:"db_url,omitempty"`
	RedisURL string            `mapstructure:"redis_url" json:"redis_url,omitempty"`
	PythonPath string          `mapstructure:"python_path" json:"python_path,omitempty"`
	ScriptDir  string          `mapstructure:"script_dir" json:"script_dir,omitempty"`
	Options  map[string]string `mapstructure:"options" json:"options,omitempty"`
}

type FactoryDeps struct {
	PostgresStore *storage.PostgresStore
	TushareStore  OHLCVStore
}

type AdapterFactory struct {
	config DataSourceConfig
	deps   FactoryDeps
	logger zerolog.Logger
	bus    *EventBus
}

func NewAdapterFactory(config DataSourceConfig, deps FactoryDeps, bus *EventBus, logger zerolog.Logger) *AdapterFactory {
	return &AdapterFactory{
		config: config,
		deps:   deps,
		logger: logger.With().Str("component", "adapter_factory").Logger(),
		bus:    bus,
	}
}

func (f *AdapterFactory) BuildPrimary() (Provider, error) {
	return f.buildProvider(f.config.Primary)
}

func (f *AdapterFactory) BuildFallback() (Provider, error) {
	if f.config.Fallback == "" {
		return nil, nil
	}
	return f.buildProvider(f.config.Fallback)
}

func (f *AdapterFactory) BuildAdapter() (*DataAdapter, error) {
	primary, err := f.BuildPrimary()
	if err != nil {
		return nil, fmt.Errorf("build primary provider %q: %w", f.config.Primary, err)
	}
	fallback, err := f.BuildFallback()
	if err != nil {
		f.logger.Warn().Err(err).Msg("fallback provider build failed, continuing without fallback")
	}
	adapter := NewDataAdapter(f.bus, primary, fallback, f.logger)

	if f.config.Cache != nil && f.config.Cache.RedisURL != "" {
		cached, err := f.wrapCache(adapter)
		if err != nil {
			f.logger.Warn().Err(err).Msg("cache layer unavailable, using uncached adapter")
			return adapter, nil
		}
		adapter = NewDataAdapter(f.bus, cached, fallback, f.logger)
	}

	return adapter, nil
}

func (f *AdapterFactory) BuildProvider(name string) (Provider, error) {
	return f.buildProvider(name)
}

func (f *AdapterFactory) wrapCache(inner Provider) (Provider, error) {
	rdb := redis.NewClient(&redis.Options{Addr: f.config.Cache.RedisURL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return NewCachedProvider(inner, rdb, *f.config.Cache, f.logger), nil
}

func (f *AdapterFactory) buildProvider(name string) (Provider, error) {
	src, ok := f.config.Sources[name]
	if !ok {
		return nil, fmt.Errorf("data source %q not found in config", name)
	}
	switch src.Type {
	case "http":
		if src.URL == "" {
			return nil, fmt.Errorf("source %q (http): url required", name)
		}
		return NewHTTPProvider(src.URL, f.logger), nil
	case "tushare":
		if src.Token == "" {
			return nil, fmt.Errorf("source %q (tushare): token required", name)
		}
		baseURL := src.URL
		if baseURL == "" {
			baseURL = "http://tushare.pro"
		}
		if f.deps.TushareStore == nil {
			return nil, fmt.Errorf("source %q (tushare): TushareStore (OHLCVStore) not injected via FactoryDeps", name)
		}
		return NewTushareProvider(src.Token, baseURL, f.deps.TushareStore, f.logger), nil
	case "postgres":
		if f.deps.PostgresStore == nil {
			return nil, fmt.Errorf("source %q (postgres): PostgresStore not injected via FactoryDeps", name)
		}
		return NewPostgresProvider(f.deps.PostgresStore, f.logger), nil
	case "akshare":
		return NewAkShareProvider(src.PythonPath, src.ScriptDir, f.logger), nil
	case "inmemory":
		return NewInMemoryProvider(), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q for source %q", src.Type, name)
	}
}

func DefaultDataSourceConfig() DataSourceConfig {
	return DataSourceConfig{
		Primary:  "http",
		Fallback: "",
		Cache: &CacheConfig{
			OHLCVTTL:       24 * time.Hour,
			FundamentalTTL: 6 * time.Hour,
			StocksTTL:      time.Hour,
			CalendarTTL:    24 * time.Hour,
			Prefix:         "md:",
		},
		Sources: map[string]SourceConfig{},
	}
}
