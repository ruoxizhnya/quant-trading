package alert

import (
	"fmt"
	"time"
)

// Rule identifiers for the 6 built-in detectors. Exported as constants so
// tests, metrics labels, and webhook consumers can reference the rule
// names without hardcoding strings.
const (
	RulePositionConcentration = "position_concentration"
	RuleSectorConcentration   = "sector_concentration"
	RuleDrawdown              = "drawdown"
	RuleDailyLossLimit        = "daily_loss_limit"
	RuleOrderFailureRate      = "order_failure_rate"
	RuleRiskMetricBreach      = "risk_metric_breach"
)

// evaluatePositionConcentration fires when a single position's market
// value exceeds MaxPositionWeight of the portfolio. Per-position alert
// is generated; the manager dispatches each separately.
func evaluatePositionConcentration(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if cfg.MaxPositionWeight <= 0 {
		return nil
	}
	threshold := snap.TotalValue * cfg.MaxPositionWeight
	alerts := make([]Alert, 0, 4)
	for _, p := range snap.Positions {
		if p.MarketValue > threshold {
			weight := p.MarketValue / snap.TotalValue
			alerts = append(alerts, Alert{
				Rule:      RulePositionConcentration,
				Severity:  severityForBreach(weight, cfg.MaxPositionWeight),
				Symbol:    p.Symbol,
				Sector:    p.Sector,
				Value:     weight,
				Threshold: cfg.MaxPositionWeight,
				Message: fmt.Sprintf(
					"position %s exceeds concentration limit: %.1f%% > %.1f%% (mv=%.2f, portfolio=%.2f)",
					p.Symbol, weight*100, cfg.MaxPositionWeight*100,
					p.MarketValue, snap.TotalValue,
				),
				Attributes: map[string]interface{}{
					"market_value":   p.MarketValue,
					"unrealized_pnl": p.UnrealizedPnL,
				},
			})
		}
	}
	return alerts
}

// evaluateSectorConcentration fires when a single sector's aggregate
// market value exceeds MaxSectorWeight of the portfolio. Multiple
// positions in the same sector are summed. Positions with empty Sector
// are bucketed into "uncategorized" but typically do not trigger unless
// the operator has a low MaxSectorWeight.
func evaluateSectorConcentration(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if cfg.MaxSectorWeight <= 0 {
		return nil
	}
	bySector := make(map[string]float64, 8)
	for _, p := range snap.Positions {
		sector := p.Sector
		if sector == "" {
			sector = "uncategorized"
		}
		bySector[sector] += p.MarketValue
	}

	threshold := snap.TotalValue * cfg.MaxSectorWeight
	alerts := make([]Alert, 0, len(bySector))
	for sector, mv := range bySector {
		if mv > threshold {
			weight := mv / snap.TotalValue
			alerts = append(alerts, Alert{
				Rule:      RuleSectorConcentration,
				Severity:  severityForBreach(weight, cfg.MaxSectorWeight),
				Sector:    sector,
				Value:     weight,
				Threshold: cfg.MaxSectorWeight,
				Message: fmt.Sprintf(
					"sector %s exceeds concentration limit: %.1f%% > %.1f%% (mv=%.2f, portfolio=%.2f)",
					sector, weight*100, cfg.MaxSectorWeight*100,
					mv, snap.TotalValue,
				),
				Attributes: map[string]interface{}{
					"sector_market_value": mv,
				},
			})
		}
	}
	return alerts
}

// evaluateDrawdown fires when the current portfolio value is more than
// MaxDrawdown below PeakEquity. Drawdown is computed as
// (TotalValue - PeakEquity) / PeakEquity (negative when below peak).
func evaluateDrawdown(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if cfg.MaxDrawdown <= 0 || snap.PeakEquity <= 0 {
		return nil
	}
	drawdown := (snap.TotalValue - snap.PeakEquity) / snap.PeakEquity
	// drawdown is negative when below peak; compare |drawdown| to limit.
	if -drawdown > cfg.MaxDrawdown {
		return []Alert{{
			Rule:      RuleDrawdown,
			Severity:  severityForBreach(-drawdown, cfg.MaxDrawdown),
			Value:     -drawdown,
			Threshold: cfg.MaxDrawdown,
			Message: fmt.Sprintf(
				"portfolio drawdown exceeds limit: %.1f%% > %.1f%% (current=%.2f, peak=%.2f)",
				-drawdown*100, cfg.MaxDrawdown*100,
				snap.TotalValue, snap.PeakEquity,
			),
		}}
	}
	return nil
}

// evaluateDailyLoss fires when the day's P&L is below DailyLossLimit
// (a negative number, e.g. -50000). The limit is a "floor" — losses
// below it trigger an alert.
func evaluateDailyLoss(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if cfg.DailyLossLimit >= 0 {
		// 0 disables the rule (we use 0 as "unset"); positive would be
		// a profit target, not a loss floor.
		return nil
	}
	if snap.DailyPnL < cfg.DailyLossLimit {
		return []Alert{{
			Rule:      RuleDailyLossLimit,
			Severity:  severityForBreach(-snap.DailyPnL, -cfg.DailyLossLimit),
			Value:     snap.DailyPnL,
			Threshold: cfg.DailyLossLimit,
			Message: fmt.Sprintf(
				"daily P&L below limit: %.2f < %.2f",
				snap.DailyPnL, cfg.DailyLossLimit,
			),
		}}
	}
	return nil
}

// evaluateOrderFailureRate counts Failed orders within the trailing
// FailureRateWindow and fires when the failure rate exceeds
// FailureRateLimit. The window defaults to 1h when unset.
func evaluateOrderFailureRate(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if cfg.FailureRateLimit <= 0 || len(snap.RecentOrders) == 0 {
		return nil
	}
	window := cfg.FailureRateWindow
	if window <= 0 {
		window = time.Hour
	}
	cutoff := time.Now().Add(-window)

	var total, failed int
	for _, o := range snap.RecentOrders {
		if o.Timestamp.Before(cutoff) {
			continue
		}
		total++
		if o.Failed {
			failed++
		}
	}
	if total == 0 {
		return nil
	}
	rate := float64(failed) / float64(total)
	if rate > cfg.FailureRateLimit {
		return []Alert{{
			Rule:      RuleOrderFailureRate,
			Severity:  severityForBreach(rate, cfg.FailureRateLimit),
			Value:     rate,
			Threshold: cfg.FailureRateLimit,
			Message: fmt.Sprintf(
				"order failure rate exceeds limit: %.1f%% > %.1f%% (%d/%d in %s window)",
				rate*100, cfg.FailureRateLimit*100,
				failed, total, window,
			),
			Attributes: map[string]interface{}{
				"failed_count": failed,
				"total_count":  total,
				"window":       window.String(),
			},
		}}
	}
	return nil
}

// evaluateRiskMetricBreaches iterates the RiskMetricThresholds map and
// fires one alert per metric whose value (from snap.RiskMetrics)
// exceeds the threshold. Unknown metrics (present in snap but not in
// thresholds) are ignored — by design, detectors only enforce rules
// the operator has configured.
func evaluateRiskMetricBreaches(snap PortfolioSnapshot, cfg AlertManagerConfig) []Alert {
	if len(cfg.RiskMetricThresholds) == 0 {
		return nil
	}
	alerts := make([]Alert, 0, len(cfg.RiskMetricThresholds))
	for name, threshold := range cfg.RiskMetricThresholds {
		value, ok := snap.RiskMetrics[name]
		if !ok {
			continue
		}
		if value > threshold {
			alerts = append(alerts, Alert{
				Rule:      RuleRiskMetricBreach,
				Severity:  severityForBreach(value, threshold),
				Value:     value,
				Threshold: threshold,
				Message: fmt.Sprintf(
					"risk metric %s exceeds threshold: %.4f > %.4f",
					name, value, threshold,
				),
				Attributes: map[string]interface{}{
					"metric": name,
				},
			})
		}
	}
	return alerts
}

// severityForBreach escalates the alert level when the breach magnitude
// is large relative to the threshold. Specifically:
//   - 1.0–1.5x threshold: warning
//   - 1.5–2.5x threshold: warning (still warning; ops should act)
//   - > 2.5x threshold:   critical (immediate action required)
//
// Returns SeverityWarning for ratios <= 1.0 so callers can safely
// pre-filter with their own thresholds if needed.
func severityForBreach(value, threshold float64) Severity {
	if threshold <= 0 {
		return SeverityWarning
	}
	ratio := value / threshold
	switch {
	case ratio >= 2.5:
		return SeverityCritical
	case ratio >= 1.0:
		return SeverityWarning
	default:
		return SeverityInfo
	}
}
