// Package main — registry construction for the data service.
//
// buildDataSourceRegistry wires every DataSourceAdapter into a single
// source.Registry, then configures the fallback chains. Adapters that
// require external configuration (API keys, transports) are constructed
// defensively: a missing dependency yields a disabled adapter rather
// than a startup failure.
package main

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/data/source"
)

// buildDataSourceRegistry constructs the multi-source registry used by
// the data service. Order of registration matters only insofar as it
// determines the default fallback chain for any data type that more
// than one adapter claims to support; this function then overrides the
// chains explicitly via registry.SetChain to make the priority
// deterministic.
//
// Adapters that need live external configuration (mootdx SDK, Alpha
// Vantage key, etc.) are constructed defensively — if the configuration
// is missing the adapter is registered as disabled. This keeps the
// service running in degraded mode instead of failing at startup.
func buildDataSourceRegistry(tushareClient *data.TushareClient, logger zerolog.Logger) *source.Registry {
	reg := source.NewRegistry()

	// 1. Tushare (existing, refactored as adapter).
	// NewTushareAdapter handles a nil client by setting Enabled()=false,
	// so the registry safely skips it at Fetch time. No outer nil check
	// needed — that would just be code duplication.
	if err := reg.Register(source.NewTushareAdapter(tushareClient)); err != nil {
		logger.Warn().Err(err).Msg("failed to register tushare adapter")
	}

	// 2. Eastmoney push2 — capital flow only.
	// The HTTP client is created unconditionally; the adapter is
	// enabled by default. To disable at runtime, set
	// DATA_DISABLE_EASTMONEY=1 in the environment.
	emClient := source.NewEastmoneyClient()
	if !envDisabled("DATA_DISABLE_EASTMONEY") {
		if err := reg.Register(source.NewEastmoneyAdapter(emClient)); err != nil {
			logger.Warn().Err(err).Msg("failed to register eastmoney adapter")
		}
		if err := reg.Register(source.NewEastmoneySectorsAdapter(emClient)); err != nil {
			logger.Warn().Err(err).Msg("failed to register eastmoney_sectors adapter")
		}
		if err := reg.Register(source.NewEastmoneyTopListAdapter(emClient)); err != nil {
			logger.Warn().Err(err).Msg("failed to register eastmoney_toplist adapter")
		}
	}

	// 3. mootdx (realtime/1min/5min). Requires the mootdx Go SDK
	// (github.com/qmaru/go-mootdx) which is not yet in go.mod. We
	// pass a nil transport so the adapter is registered as disabled
	// (Enabled()==false → Registry.Fetch skips it). When the SDK is
	// added, replace nil with a real transport wrapper.
	mootdxAdapter := source.NewMootdxAdapter(nil)
	if !envDisabled("DATA_DISABLE_MOOTDX") {
		if err := reg.Register(mootdxAdapter); err != nil {
			logger.Warn().Err(err).Msg("failed to register mootdx adapter")
		}
	}

	// 4. Juchao (announcements). No credentials required.
	if !envDisabled("DATA_DISABLE_JUCHAO") {
		if err := reg.Register(source.NewJuchaoAdapter()); err != nil {
			logger.Warn().Err(err).Msg("failed to register juchao adapter")
		}
	}

	// 5. Xueqiu (hot search / news). No credentials required.
	if !envDisabled("DATA_DISABLE_XUEQIU") {
		if err := reg.Register(source.NewXueqiuAdapter()); err != nil {
			logger.Warn().Err(err).Msg("failed to register xueqiu adapter")
		}
	}

	// 6. Alpha Vantage — requires ALPHA_VANTAGE_KEY. Without it the
	// adapter is constructed as disabled; it is still registered so
	// that /api/datasource/registry reports the slot as "configured
	// but not active".
	//
	// CR-54 (ODR-012): the precedence between env and viper was
	// previously silent. Operators setting ALPHA_VANTAGE_KEY in the
	// environment would be confused when their config file's
	// alpha_vantage.api_key silently overrode it. We now log a
	// warning whenever both sources are set, regardless of whether
	// the values agree, so the precedence is auditable. (Viper wins
	// in both cases because it is the more specific config layer;
	// the warning is informational, not a config conflict error.)
	envAlphaKey := strings.TrimSpace(os.Getenv("ALPHA_VANTAGE_KEY"))
	viperAlphaKey := ""
	if viper.IsSet("alpha_vantage.api_key") {
		viperAlphaKey = strings.TrimSpace(viper.GetString("alpha_vantage.api_key"))
	}
	alphaKey := envAlphaKey
	if viperAlphaKey != "" {
		alphaKey = viperAlphaKey
	}
	if envAlphaKey != "" && viperAlphaKey != "" && envAlphaKey != viperAlphaKey {
		logger.Warn().
			Str("env_value", maskSecret(envAlphaKey)).
			Str("viper_value", maskSecret(viperAlphaKey)).
			Msg("ALPHA_VANTAGE_KEY is set in BOTH env and viper config with different values; viper wins (config-file takes precedence over env). Verify this is intended.")
	} else if envAlphaKey != "" && viperAlphaKey != "" {
		logger.Info().Msg("ALPHA_VANTAGE_KEY set in both env and viper config with the same value; viper wins silently (no functional difference)")
	}
	if !envDisabled("DATA_DISABLE_ALPHA_VANTAGE") {
		if err := reg.Register(source.NewAlphaVantageAdapter(alphaKey)); err != nil {
			logger.Warn().Err(err).Msg("failed to register alpha_vantage adapter")
		}
	}

	// 7. Yahoo Finance — public API, enabled by default. Rate limit
	// is conservative (60 req/min via the adapter's RateLimit()).
	if !envDisabled("DATA_DISABLE_YAHOO") {
		if err := reg.Register(source.NewYahooFinanceAdapter()); err != nil {
			logger.Warn().Err(err).Msg("failed to register yahoo_finance adapter")
		}
	}

	// Configure explicit fallback chains. The default chain built by
	// Registry.Register appends adapters in registration order, but
	// for the most important data types we want a precise priority.
	configureFallbackChains(reg)

	logger.Info().
		Int("adapters", len(reg.ListAdapters())).
		Int("chains", len(reg.ListChains())).
		Strs("names", reg.ListAdapters()).
		Msg("multi-source data registry initialized")

	return reg
}

// configureFallbackChains sets the per-data-type priority used by
// Registry.Fetch. The first element is the primary; subsequent
// elements are tried on retryable failure.
//
// Design notes:
//
//   - Daily OHLCV: tushare (authoritative, with full history) is
//     primary. mootdx is the secondary for symbols tushare cannot
//     serve. eastmoney is the tertiary.
//
//   - Realtime quote: mootdx is primary (5-level depth, low latency).
//     Eastmoney push2 is the fallback when the SDK is unavailable.
//
//   - Capital flow: eastmoney is the only first-class source today.
//     The chain contains just one entry; tushare's flow is separate
//     and not served by this registry.
//
//   - Sectors / top_list: dedicated eastmoney adapters.
//
//   - Global OHLCV: yahoo is primary (rich history, no key needed);
//     alpha_vantage is the secondary.
func configureFallbackChains(reg *source.Registry) {
	reg.SetChain(source.DataTypeOHLCDaily, []string{
		"tushare",
		"mootdx",
		"eastmoney",
	})
	reg.SetChain(source.DataTypeOHLCMinute, []string{
		"mootdx",
		"eastmoney",
	})
	reg.SetChain(source.DataTypeRealtime, []string{
		"mootdx",
		"eastmoney",
	})
	reg.SetChain(source.DataTypeCapitalFlow, []string{
		"eastmoney",
	})
	reg.SetChain(source.DataTypeFundamental, []string{
		"tushare",
	})
	reg.SetChain(source.DataTypeSectors, []string{
		"eastmoney_sectors",
	})
	reg.SetChain(source.DataTypeStockSector, []string{
		"eastmoney_sectors",
	})
	reg.SetChain(source.DataTypeTopList, []string{
		"eastmoney_toplist",
	})
	reg.SetChain(source.DataTypeLimitUpPool, []string{
		"eastmoney_toplist",
	})
	reg.SetChain(source.DataTypeAnnounce, []string{
		"juchao",
	})
	reg.SetChain(source.DataTypeNews, []string{
		"xueqiu",
	})
	reg.SetChain(source.DataTypeHotSearch, []string{
		"xueqiu",
	})
	reg.SetChain(source.DataTypeGlobalOHLCV, []string{
		"yahoo_finance",
		"alpha_vantage",
	})
}

// envDisabled returns true if the named environment variable is set
// to a truthy value ("1", "true", "yes", "on", case-insensitive).
// Used as a kill switch for individual adapters in production.
func envDisabled(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// maskSecret returns a redacted form of a secret for log output.
// Keeps the first 4 and last 2 characters visible (if available) so
// operators can still tell "they are the same value" vs "completely
// different values" while not leaking the key material. Strings
// shorter than 8 characters are fully redacted.
//
// Used by CR-54 (ODR-012) to log ALPHA_VANTAGE_KEY precedence
// warnings without putting the actual key in stdout/journald.
func maskSecret(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-2:]
}
