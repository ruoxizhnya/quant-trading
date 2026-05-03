package backtest

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

type stockJob struct {
	symbol    string
	prevClose float64
}

type stockResult struct {
	symbol    string
	stock     domain.Stock
	ohlcvData []domain.OHLCV
	price     float64
	prevClose float64
	limitUp   bool
	limitDown bool
	err       error
}

type corporateActions struct {
	dividendsByDate map[time.Time][]*domain.Dividend
	splitsByDate    map[time.Time][]*domain.Split
}

func (e *Engine) loadCorporateActions(ctx context.Context, start, end time.Time, logger zerolog.Logger) *corporateActions {
	ca := &corporateActions{
		dividendsByDate: make(map[time.Time][]*domain.Dividend),
		splitsByDate:    make(map[time.Time][]*domain.Split),
	}

	e.mu.RLock()
	store := e.store
	e.mu.RUnlock()

	if store == nil {
		return ca
	}

	if divs, err := store.GetDividendsInRange(ctx, start, end); err == nil {
		for _, d := range divs {
			day := d.PayDate.Truncate(24 * time.Hour)
			ca.dividendsByDate[day] = append(ca.dividendsByDate[day], d)
		}
		logger.Info().Int("dividend_events", len(divs)).Msg("Dividend data loaded")
	}

	if splits, err := store.GetSplitsInRange(ctx, start, end); err == nil {
		for _, s := range splits {
			day := s.TradeDate.Truncate(24 * time.Hour)
			ca.splitsByDate[day] = append(ca.splitsByDate[day], s)
		}
		logger.Info().Int("split_events", len(splits)).Msg("Split data loaded")
	}

	return ca
}

func (e *Engine) fetchMarketDataForDay(
	ctx context.Context,
	stockPool []string,
	params domain.BacktestParams,
	date time.Time,
	prevCloseCache map[string]float64,
	logger zerolog.Logger,
) (
	marketDataCache map[string][]domain.OHLCV,
	pricesCache map[string]float64,
	stockCache map[string]domain.Stock,
	updatedPrevClose map[string]float64,
) {
	marketDataCache = make(map[string][]domain.OHLCV)
	pricesCache = make(map[string]float64)
	stockCache = make(map[string]domain.Stock)
	updatedPrevClose = make(map[string]float64)

	for k, v := range prevCloseCache {
		updatedPrevClose[k] = v
	}

	workers := e.parallelWorkers
	if workers <= 0 {
		workers = 1
	}

	jobCh := make(chan stockJob, len(stockPool))
	resultCh := make(chan stockResult, len(stockPool))

	var stockWg sync.WaitGroup
	for w := 0; w < workers; w++ {
		stockWg.Add(1)
		go func() {
			defer stockWg.Done()
			for job := range jobCh {
				res := e.processStockJob(ctx, job, params, date)
				resultCh <- res
			}
		}()
	}

	for _, s := range stockPool {
		jobCh <- stockJob{symbol: s, prevClose: prevCloseCache[s]}
	}
	close(jobCh)

	for i := 0; i < len(stockPool); i++ {
		res := <-resultCh
		if res.err != nil {
			logger.Warn().Err(res.err).Msg("Failed to get stock data")
			continue
		}
		stockCache[res.symbol] = res.stock
		marketDataCache[res.symbol] = res.ohlcvData
		if res.price > 0 {
			pricesCache[res.symbol] = res.price
		}
		if res.prevClose > 0 {
			updatedPrevClose[res.symbol] = res.prevClose
		}
	}
	stockWg.Wait()

	return marketDataCache, pricesCache, stockCache, updatedPrevClose
}

func (e *Engine) processStockJob(
	ctx context.Context,
	job stockJob,
	params domain.BacktestParams,
	date time.Time,
) stockResult {
	res := stockResult{symbol: job.symbol}

	if stock, err := e.getStock(ctx, job.symbol); err == nil {
		res.stock = stock
	}

	ohlcvData, err := e.getOHLCV(ctx, job.symbol, params.StartDate, date)
	if err != nil {
		res.err = apperrors.Wrapf(err, apperrors.ErrCodeDataQuality, "fetch_ohlcv", "symbol %s failed to fetch OHLCV", job.symbol)
		return res
	}
	res.ohlcvData = ohlcvData

	limitUp, limitDown := false, false
	limitPrice := 0.0
	tradeDays := 0
	stockName := res.stock.Name
	if !res.stock.ListDate.IsZero() {
		tradeDays = int(date.Sub(res.stock.ListDate).Hours() / 24 / 7 * 5)
	}

	prevClose := job.prevClose
	if len(ohlcvData) >= 2 {
		if prevClose <= 0 {
			prevClose = ohlcvData[len(ohlcvData)-2].Close
		}
		if prevClose > 0 {
			limitRate := e.config.Trading.PriceLimit.Normal
			if tradeDays < e.config.Trading.NewStockDays {
				limitRate = e.config.Trading.PriceLimit.New
			} else if hasSTPrefix(stockName) {
				limitRate = e.config.Trading.PriceLimit.ST
			}
			todayBar := ohlcvData[len(ohlcvData)-1]
			upperLimit := prevClose * (1 + limitRate)
			lowerLimit := prevClose * (1 - limitRate)
			limitUp = todayBar.Close >= upperLimit
			limitDown = todayBar.Close <= lowerLimit
			if limitUp {
				limitPrice = upperLimit
			} else if limitDown {
				limitPrice = lowerLimit
			}
			todayBar.LimitUp = limitUp
			todayBar.LimitDown = limitDown
			ohlcvData[len(ohlcvData)-1] = todayBar
			res.ohlcvData = ohlcvData
			if limitUp || limitDown {
				res.prevClose = todayBar.Close
			} else {
				res.prevClose = ohlcvData[len(ohlcvData)-1].Close
			}
		}
	}

	if len(ohlcvData) > 0 {
		if limitPrice > 0 {
			res.price = limitPrice
		} else {
			res.price = ohlcvData[len(ohlcvData)-1].Close
		}
	}
	res.limitUp = limitUp
	res.limitDown = limitDown

	return res
}

func (e *Engine) processSignalsAndExecuteTrades(
	ctx context.Context,
	state *BacktestState,
	signals []domain.Signal,
	marketDataCache map[string][]domain.OHLCV,
	pricesCache map[string]float64,
	regime *domain.MarketRegime,
	date time.Time,
	logger zerolog.Logger,
) {
	if len(signals) == 0 {
		return
	}

	validSignals := make([]domain.Signal, 0, len(signals))
	for _, sig := range signals {
		if !sig.Date.Before(date) {
			validSignals = append(validSignals, sig)
		}
	}
	if len(validSignals) == 0 {
		return
	}

	portfolio := state.Tracker.GetPortfolio(pricesCache)

	positionSizes, err := e.calculatePositionsBatch(ctx, validSignals, portfolio, regime, pricesCache, marketDataCache)
	if err != nil {
		logger.Warn().Err(err).Msg("Batch position sizing failed, falling back to per-signal")
		e.processSignalsFallback(ctx, state, validSignals, marketDataCache, pricesCache, regime, date, logger)
		return
	}

	for _, signal := range validSignals {
		var todayBar *domain.OHLCV
		if ohlcvData, ok := marketDataCache[signal.Symbol]; ok && len(ohlcvData) > 0 {
			todayBar = &ohlcvData[len(ohlcvData)-1]
			if todayBar.LimitUp && (signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort) {
				logger.Info().
					Str("symbol", signal.Symbol).
					Str("direction", string(signal.Direction)).
					Time("date", date).
					Msg("Trade blocked: stock hit limit-up (涨停), cannot buy")
				continue
			}
			if todayBar.LimitDown && signal.Direction == domain.DirectionClose {
				logger.Info().
					Str("symbol", signal.Symbol).
					Time("date", date).
					Msg("Trade blocked: stock hit limit-down (跌停), cannot sell")
				continue
			}
		}

		execOpts := &OrderExecutionOpts{
			OrderType:  signal.OrderType,
			LimitPrice: signal.LimitPrice,
			DayBar:     todayBar,
		}

		positionSize, hasSize := positionSizes[signal.Symbol]
		if !hasSize || positionSize.Size <= 0 {
			continue
		}

		targetQty := positionSize.Size

		tp, exists := state.targetPositions[signal.Symbol]
		if !exists {
			tp = &domain.TargetPosition{
				Symbol:      signal.Symbol,
				TargetQty:   0,
				ActualQty:   0,
				PendingQty:  0,
				LastUpdated: date,
			}
			state.targetPositions[signal.Symbol] = tp
		}

		effectiveTarget := e.computeEffectiveTarget(tp, targetQty, signal.Direction, date, logger)
		if effectiveTarget < 0 {
			continue
		}

		tp.TargetQty = targetQty
		tp.LastUpdated = date

		if effectiveTarget > 0 {
			e.executeSignalTrade(state, signal, effectiveTarget, pricesCache, date, execOpts, tp, logger)
		}
	}
}

func (e *Engine) processSignalsFallback(
	ctx context.Context,
	state *BacktestState,
	signals []domain.Signal,
	marketDataCache map[string][]domain.OHLCV,
	pricesCache map[string]float64,
	regime *domain.MarketRegime,
	date time.Time,
	logger zerolog.Logger,
) {
	for _, signal := range signals {
		var todayBar *domain.OHLCV
		if ohlcvData, ok := marketDataCache[signal.Symbol]; ok && len(ohlcvData) > 0 {
			todayBar = &ohlcvData[len(ohlcvData)-1]
			if todayBar.LimitUp && (signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort) {
				continue
			}
			if todayBar.LimitDown && signal.Direction == domain.DirectionClose {
				continue
			}
		}
		execOpts := &OrderExecutionOpts{
			OrderType:  signal.OrderType,
			LimitPrice: signal.LimitPrice,
			DayBar:     todayBar,
		}
		portfolio := state.Tracker.GetPortfolio(pricesCache)
		currentPrice := pricesCache[signal.Symbol]
		positionSize, err := e.calculatePosition(ctx, signal, portfolio, regime, currentPrice)
		if err != nil || positionSize.Size <= 0 {
			continue
		}
		targetQty := positionSize.Size
		tp, exists := state.targetPositions[signal.Symbol]
		if !exists {
			tp = &domain.TargetPosition{
				Symbol:      signal.Symbol,
				TargetQty:   0,
				ActualQty:   0,
				PendingQty:  0,
				LastUpdated: date,
			}
			state.targetPositions[signal.Symbol] = tp
		}
		effectiveTarget := e.computeEffectiveTarget(tp, targetQty, signal.Direction, date, logger)
		if effectiveTarget < 0 {
			continue
		}
		tp.TargetQty = targetQty
		tp.LastUpdated = date
		if effectiveTarget > 0 {
			e.executeSignalTrade(state, signal, effectiveTarget, pricesCache, date, execOpts, tp, logger)
		}
	}
}

func (e *Engine) computeEffectiveTarget(
	tp *domain.TargetPosition,
	targetQty float64,
	direction domain.Direction,
	date time.Time,
	logger zerolog.Logger,
) float64 {
	effectiveTarget := targetQty
	if tp.PendingQty > 0 && (direction == domain.DirectionLong || direction == domain.DirectionShort) {
		if tp.ActualQty >= targetQty {
			effectiveTarget = 0
		} else {
			effectiveTarget = targetQty - tp.ActualQty
		}
		if effectiveTarget <= 0 {
			logger.Info().
				Str("symbol", tp.Symbol).
				Float64("actual_qty", tp.ActualQty).
				Float64("pending_qty", tp.PendingQty).
				Float64("new_target", targetQty).
				Time("date", date).
				Msg("Signal skipped: already at or above target")
			return -1
		}
		if effectiveTarget < targetQty {
			logger.Info().
				Str("symbol", tp.Symbol).
				Float64("actual_qty", tp.ActualQty).
				Float64("pending_qty", tp.PendingQty).
				Float64("new_target", targetQty).
				Float64("effective_target", effectiveTarget).
				Time("date", date).
				Msg("Adjusted target: netting actual owned qty")
		}
	}
	return effectiveTarget
}

func (e *Engine) executeSignalTrade(
	state *BacktestState,
	signal domain.Signal,
	effectiveTarget float64,
	pricesCache map[string]float64,
	date time.Time,
	execOpts *OrderExecutionOpts,
	tp *domain.TargetPosition,
	logger zerolog.Logger,
) {
	price := pricesCache[signal.Symbol]
	if price <= 0 {
		return
	}

	var trade *domain.Trade
	var err error

	switch signal.Direction {
	case domain.DirectionLong:
		trade, err = state.Tracker.ExecuteTrade(
			signal.Symbol,
			domain.DirectionLong,
			effectiveTarget,
			price,
			date,
			execOpts,
		)
		if err != nil {
			logger.Warn().
				Str("symbol", signal.Symbol).
				Err(err).
				Msg("Failed to execute long trade")
			tp.PendingQty = tp.TargetQty - tp.ActualQty
			tp.LastUpdated = date
			return
		}

	case domain.DirectionShort:
		trade, err = state.Tracker.ExecuteTrade(
			signal.Symbol,
			domain.DirectionShort,
			effectiveTarget,
			price,
			date,
			execOpts,
		)
		if err != nil {
			logger.Warn().
				Str("symbol", signal.Symbol).
				Err(err).
				Msg("Failed to execute short trade")
			tp.PendingQty = tp.TargetQty - tp.ActualQty
			tp.LastUpdated = date
			return
		}

	case domain.DirectionClose:
		trade, err = state.Tracker.ClosePosition(signal.Symbol, price, date)
		if err != nil {
			logger.Warn().
				Str("symbol", signal.Symbol).
				Err(err).
				Msg("Failed to close position")
			return
		}
		tp.PendingQty = 0
		tp.TargetQty = 0
		tp.ActualQty = 0
		tp.LastUpdated = date
		trade.PendingQty = 0
		delete(state.targetPositions, signal.Symbol)
		return

	case domain.DirectionHold:
		return
	}

	if trade != nil {
		e.updateTargetPositionAfterTrade(state, trade, signal, tp, date, logger)
	}
}

func (e *Engine) updateTargetPositionAfterTrade(
	state *BacktestState,
	trade *domain.Trade,
	signal domain.Signal,
	tp *domain.TargetPosition,
	date time.Time,
	logger zerolog.Logger,
) {
	if signal.Direction == domain.DirectionLong || signal.Direction == domain.DirectionShort {
		tp.ActualQty += trade.Quantity
		tp.PendingQty = tp.TargetQty - tp.ActualQty
		trade.PendingQty = tp.PendingQty
		if tp.PendingQty > 0 {
			logger.Info().
				Str("symbol", signal.Symbol).
				Float64("target_qty", tp.TargetQty).
				Float64("actual_qty", tp.ActualQty).
				Float64("pending_qty", tp.PendingQty).
				Time("date", date).
				Msg("Partial fill: target vs actual gap recorded")
		}
	} else if signal.Direction == domain.DirectionClose {
		tp.PendingQty = 0
		tp.TargetQty = 0
		tp.ActualQty = 0
		trade.PendingQty = 0
	}
	tp.LastUpdated = date

	if tp.PendingQty <= 0 && tp.TargetQty <= 0 {
		delete(state.targetPositions, signal.Symbol)
	}
}

func (e *Engine) processStopLosses(
	state *BacktestState,
	pricesCache map[string]float64,
	marketDataCache map[string][]domain.OHLCV,
	date time.Time,
	logger zerolog.Logger,
) {
	e.mu.RLock()
	rm := e.riskManager
	e.mu.RUnlock()

	if rm == nil {
		return
	}

	slChecker := rm.GetStopLossChecker()

	var precomputedATR map[string]float64
	if slChecker != nil && len(marketDataCache) > 0 {
		precomputedATR, _ = slChecker.ATRFromOHLCV(marketDataCache)
	}

	stopLossEvents, err := e.checkStopLossesWithATR(context.Background(), state.Tracker, pricesCache, precomputedATR)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to check stop losses")
	}

	for _, event := range stopLossEvents {
		if event.Type == "stop_loss" || event.Type == "take_profit" {
			_, err := state.Tracker.ExecuteTrade(
				event.Symbol,
				domain.DirectionClose,
				event.Quantity,
				event.Price,
				date,
				nil,
			)
			if err != nil {
				logger.Warn().
					Str("symbol", event.Symbol).
					Str("type", event.Type).
					Err(err).
					Msg("Failed to execute stop loss")
			}
		}
	}
}

func (e *Engine) processCorporateActions(
	state *BacktestState,
	ca *corporateActions,
	date time.Time,
	logger zerolog.Logger,
) {
	truncatedDate := date.Truncate(24 * time.Hour)
	if divs, ok := ca.dividendsByDate[truncatedDate]; ok {
		for _, d := range divs {
			if err := state.Tracker.ProcessDividend(d.Symbol, *d); err != nil {
				logger.Warn().Str("symbol", d.Symbol).Err(err).Msg("Failed to process dividend")
			}
		}
	}
	if splits, ok := ca.splitsByDate[truncatedDate]; ok {
		for _, s := range splits {
			if err := state.Tracker.ProcessSplit(s.Symbol, *s); err != nil {
				logger.Warn().Str("symbol", s.Symbol).Err(err).Msg("Failed to process split")
			}
		}
	}
}

func (e *Engine) forceCloseAllPositions(
	state *BacktestState,
	pricesCache map[string]float64,
	lastTradingDay time.Time,
	logger zerolog.Logger,
) {
	for symbol, pos := range state.Tracker.GetAllPositions() {
		if abs(pos.Quantity) > 1e-8 {
			price, priceExists := pricesCache[symbol]
			if !priceExists || price <= 0 {
				if pos.CurrentPrice > 0 {
					price = pos.CurrentPrice
					logger.Info().
						Str("symbol", symbol).
						Float64("qty", pos.Quantity).
						Float64("fallback_price", price).
						Time("date", lastTradingDay).
						Msg("Using current price as fallback for force close")
				} else {
					logger.Warn().
						Str("symbol", symbol).
						Float64("qty", pos.Quantity).
						Time("date", lastTradingDay).
						Msg("Skipping force close: no price data for symbol at backtest end")
					continue
				}
			}
			closeTrade, err := state.Tracker.ClosePosition(symbol, price, lastTradingDay)
			if err != nil {
				logger.Warn().
					Str("symbol", symbol).
					Err(err).
					Time("date", lastTradingDay).
					Msg("Failed to force close position at backtest end")
			} else if closeTrade != nil {
				logger.Info().
					Str("symbol", symbol).
					Float64("qty", closeTrade.Quantity).
					Float64("price", closeTrade.Price).
					Time("date", lastTradingDay).
					Msg("Force closed position at backtest end")
			}
		}
	}
}
