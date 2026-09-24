package backtest

import (
	"context"
	"fmt"
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

// stockNamesFor projects the day's stock metadata down to the
// symbol -> display-name table the tracker needs for risk-warning
// detection (AUD-22).
//
// Symbols whose Stock could not be fetched are included with an EMPTY
// name rather than dropped: the tracker treats an empty name as "unknown"
// and logs it, whereas a missing key would be indistinguishable from
// "this backtest has no risk-warning stocks". An empty value keeps that
// distinction visible at the point where it matters.
func stockNamesFor(stockCache map[string]domain.Stock) map[string]string {
	names := make(map[string]string, len(stockCache))
	for symbol, stock := range stockCache {
		names[symbol] = stock.Name
	}
	return names
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
			// AUD-07 (ODR-065 H2): board-aware, date-aware limit rate.
			// The previous inline if/else knew only Normal/ST/New and
			// applied 10% to ChiNext/STAR/BSE symbols. See
			// pricelimit.go for the precedence rules.
			limitRate := resolvePriceLimit(
				PriceLimitInput{
					Symbol:    job.symbol,
					Name:      stockName,
					TradeDays: tradeDays,
					AsOf:      date,
				},
				PriceLimitConfigValues{
					Normal:       e.config.Trading.PriceLimit.Normal,
					ST:           e.config.Trading.PriceLimit.ST,
					STBefore:     e.config.Trading.PriceLimit.STBefore,
					New:          e.config.Trading.PriceLimit.New,
					NewStockDays: e.config.Trading.NewStockDays,
				},
			)
			todayBar := ohlcvData[len(ohlcvData)-1]
			// AUD-07: round the limit prices to the cent before
			// comparing. Exchanges publish limit prices at 0.01 tick,
			// so an unrounded bound can flip the verdict on a bar that
			// closes exactly at the limit.
			upperLimit, lowerLimit := LimitPrices(prevClose, limitRate)
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

		tp, exists := state.TargetPositions[signal.Symbol]
		if !exists {
			tp = &domain.TargetPosition{
				Symbol:      signal.Symbol,
				TargetQty:   0,
				ActualQty:   0,
				PendingQty:  0,
				LastUpdated: date,
			}
			state.TargetPositions[signal.Symbol] = tp
		}

		// 先把策略今天的目标落账，再算实际要下的量 —— 这样即使下面 skip
		// （已达标 / 已超配），`TargetPosition` 记的也是**今天**的目标（AUD-55）。
		tp.TargetQty = targetQty
		tp.LastUpdated = date

		effectiveTarget := e.computeEffectiveTarget(state, tp, targetQty, signal.Direction, date, logger)
		if effectiveTarget < 0 {
			continue
		}

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
		tp, exists := state.TargetPositions[signal.Symbol]
		if !exists {
			tp = &domain.TargetPosition{
				Symbol:      signal.Symbol,
				TargetQty:   0,
				ActualQty:   0,
				PendingQty:  0,
				LastUpdated: date,
			}
			state.TargetPositions[signal.Symbol] = tp
		}
		tp.TargetQty = targetQty
		tp.LastUpdated = date

		effectiveTarget := e.computeEffectiveTarget(state, tp, targetQty, signal.Direction, date, logger)
		if effectiveTarget < 0 {
			continue
		}

		if effectiveTarget > 0 {
			e.executeSignalTrade(state, signal, effectiveTarget, pricesCache, date, execOpts, tp, logger)
		}
	}
}

// computeEffectiveTarget 决定一条 Long / Short 信号**实际**要下多少量。
//
// AUD-55（AUD-53 的根因）：原实现只在 `tp.PendingQty > 0` 时才做
// 「目标 − 已持」抵扣，而 `PendingQty` 是**上一次成交后写下的缓存值** ——
// 恰好达标时它等于 0、超配时它小于 0，两种情况下抵扣都不生效。配上一个
// **无状态**策略（`momentum` 每天对 top-N 重发全量 `Long`、从不读持仓），
// 后果是**每天重发一次全量买单、每天被 `insufficient cash` 拒一次**。
// 这就是 AUD-53「回测结果随价格水平 / 资金量级漂移 20 个百分点」的通道。
//
// 修法有两半，缺一不可：
//
//  1. 抵扣**无条件**执行 —— 不再看 `PendingQty` 的符号。
//  2. 已持仓**每次实时问 tracker**，不读 `tp.ActualQty` 这个缓存。
//     能改持仓的路径不止 `executeSignalTrade`：止损 / 止盈平仓、拆股、
//     退市强平都直接落在 tracker 上。逐一在每处补同步是「靠记得」，
//     漏一处就退化回原 bug；读真值则结构上不可能漏（AUD-50 的同一教训）。
//
// 返回值 < 0 表示「本条信号不产生委托」（已达标或已超配）。
func (e *Engine) computeEffectiveTarget(
	state *BacktestState,
	tp *domain.TargetPosition,
	targetQty float64,
	direction domain.Direction,
	date time.Time,
	logger zerolog.Logger,
) float64 {
	// Close / Hold 不走「目标 − 已持」：Close 是全平（`executeSignalTrade`
	// 直接调 `Tracker.ClosePosition`，不看这个数），Hold 是空信号。
	if direction != domain.DirectionLong && direction != domain.DirectionShort {
		return targetQty
	}

	held := e.heldQty(state, tp.Symbol)
	effectiveTarget := targetQty - held
	if effectiveTarget <= 0 {
		logger.Info().
			Str("symbol", tp.Symbol).
			Float64("actual_qty", held).
			Float64("pending_qty", tp.PendingQty).
			Float64("new_target", targetQty).
			Time("date", date).
			Msg("Signal skipped: already at or above target")
		return -1
	}
	if effectiveTarget < targetQty {
		logger.Info().
			Str("symbol", tp.Symbol).
			Float64("actual_qty", held).
			Float64("pending_qty", tp.PendingQty).
			Float64("new_target", targetQty).
			Float64("effective_target", effectiveTarget).
			Time("date", date).
			Msg("Adjusted target: netting actual owned qty")
	}
	return effectiveTarget
}

// heldQty 读 tracker 里的**真实**持仓量，负数（空头）按 0 处理 ——
// 本函数只服务多头 / 空头的**加仓差额**，不承担反向平仓的推理。
func (e *Engine) heldQty(state *BacktestState, symbol string) float64 {
	if state == nil || state.Tracker == nil {
		return 0
	}
	pos, ok := state.Tracker.GetPosition(symbol)
	if !ok || pos == nil || pos.Quantity < 0 {
		return 0
	}
	return pos.Quantity
}

// reconcileTargetPosition 把 `TargetPosition` 与 tracker 的真实持仓对齐。
//
// 供**引擎外**改持仓的路径调用：止损 / 止盈平仓（本文件 `processStopLosses`）、
// 拆股（`ProcessSplit`）、退市强平（`forceCloseDelisted`）。它只负责那份
// **对外可见的账**（回测结果 / 日志），不参与下单决策 —— 决策侧已经实时读
// tracker，见 `computeEffectiveTarget`。
func (e *Engine) reconcileTargetPosition(
	state *BacktestState,
	symbol string,
	date time.Time,
	logger zerolog.Logger,
) {
	tp, ok := state.TargetPositions[symbol]
	if !ok {
		return
	}
	held := e.heldQty(state, symbol)
	if held == tp.ActualQty {
		return
	}
	logger.Info().
		Str("symbol", symbol).
		Float64("stale_actual_qty", tp.ActualQty).
		Float64("actual_qty", held).
		Float64("target_qty", tp.TargetQty).
		Time("date", date).
		Msg("Target position reconciled with tracker holdings")
	tp.ActualQty = held
	tp.PendingQty = tp.TargetQty - tp.ActualQty
	tp.LastUpdated = date
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

	// Check if ExecutionService is available for more realistic execution
	// P1-17 (ADR-020): read via ExecutionBridge (the live component)
	execSvc := e.executionBridge.Get()

	useExecutionService := execSvc != nil && signal.Direction != domain.DirectionHold

	switch signal.Direction {
	case domain.DirectionLong:
		if useExecutionService {
			trade, err = e.executeViaExecutionService(state, signal, effectiveTarget, price, date, execOpts, logger)
		} else {
			trade, err = state.Tracker.ExecuteTrade(
				signal.Symbol,
				domain.DirectionLong,
				effectiveTarget,
				price,
				date,
				execOpts,
			)
		}
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
		if useExecutionService {
			trade, err = e.executeViaExecutionService(state, signal, effectiveTarget, price, date, execOpts, logger)
		} else {
			trade, err = state.Tracker.ExecuteTrade(
				signal.Symbol,
				domain.DirectionShort,
				effectiveTarget,
				price,
				date,
				execOpts,
			)
		}
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
		if useExecutionService {
			trade, err = e.executeViaExecutionService(state, signal, effectiveTarget, price, date, execOpts, logger)
		} else {
			trade, err = state.Tracker.ClosePosition(signal.Symbol, price, date)
		}
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
		if trade != nil {
			trade.PendingQty = 0
		}
		delete(state.TargetPositions, signal.Symbol)
		return

	case domain.DirectionHold:
		return
	}

	if trade != nil {
		e.updateTargetPositionAfterTrade(state, trade, signal, tp, date, logger)
	}
}

// executeViaExecutionService executes a trade through the ExecutionService.
// This provides more realistic execution with configurable slippage and commission models.
func (e *Engine) executeViaExecutionService(
	state *BacktestState,
	signal domain.Signal,
	quantity float64,
	price float64,
	date time.Time,
	execOpts *OrderExecutionOpts,
	logger zerolog.Logger,
) (*domain.Trade, error) {
	// P1-17 (ADR-020): read via ExecutionBridge.
	execSvc := e.executionBridge.Get()

	if execSvc == nil {
		return nil, fmt.Errorf("execution service not available")
	}

	// Build order
	order := domain.Order{
		Symbol:    signal.Symbol,
		Direction: signal.Direction,
		Quantity:  quantity,
		Timestamp: date,
	}

	// Set order type and limit price
	if execOpts != nil {
		order.OrderType = execOpts.OrderType
		order.LimitPrice = execOpts.LimitPrice
	} else {
		order.OrderType = domain.OrderTypeMarket
	}

	// Build quote from available data
	quote := Quote{
		Symbol: signal.Symbol,
		Close:  price,
		Date:   date,
	}

	// If we have OHLCV data, use it for more accurate execution
	if execOpts != nil && execOpts.DayBar != nil {
		quote.Open = execOpts.DayBar.Open
		quote.High = execOpts.DayBar.High
		quote.Low = execOpts.DayBar.Low
		quote.Close = execOpts.DayBar.Close
		quote.Volume = execOpts.DayBar.Volume
	}

	// Execute through service
	trade, err := execSvc.ExecuteOrder(order, quote)
	if err != nil {
		return nil, err
	}

	// Apply trade to tracker (update cash and positions)
	trackerTrade, err := state.Tracker.ApplyTrade(trade)
	if err != nil {
		return nil, err
	}

	logger.Debug().
		Str("symbol", signal.Symbol).
		Str("direction", string(signal.Direction)).
		Float64("qty", trade.Quantity).
		Float64("price", trade.Price).
		Float64("commission", trade.Commission).
		Str("slippage_model", execSvc.GetSlippageModel()).
		Msg("Trade executed via ExecutionService")

	return trackerTrade, nil
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
		delete(state.TargetPositions, signal.Symbol)
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
		if event.Type != "stop_loss" && event.Type != "take_profit" {
			continue
		}
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
			continue
		}
		// AUD-55 的耦合点：止损 / 止盈是**引擎外**平仓 —— 它直接落在 tracker 上，
		// 不经过 `executeSignalTrade` / `updateTargetPositionAfterTrade`。
		// 不在这里对齐，`TargetPosition` 那份账就会停在旧持仓上。
		e.reconcileTargetPosition(state, event.Symbol, date, logger)
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
				continue
			}
			// 拆股改的是**股数** —— 与止损平仓同属「引擎外改持仓」（AUD-55）。
			e.reconcileTargetPosition(state, s.Symbol, truncatedDate, logger)
		}
	}
}

func (e *Engine) forceCloseAllPositions(
	state *BacktestState,
	pricesCache map[string]float64,
	lastTradingDay time.Time,
	logger zerolog.Logger,
) {
	// 强平顺序也要定序：先平哪只影响当天的现金与成交序列（P1-14）。
	openPositions := state.Tracker.GetAllPositions()
	for _, symbol := range sortedKeys(openPositions) {
		pos := openPositions[symbol]
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

// forceCloseDelisted 平掉「已摘牌却还留在账上」的持仓（P2-4）。
//
// 为什么必须做：一旦把已退市的票从 universe 里剔掉，市场数据里就没有它了，
// 价格拿不到、止损触发不了，这笔持仓会一直挂到回测结束才被末尾的强平兜住
// —— 中间几十上百个交易日的资金占用和损益全被抹平，等于凭空造收益。
// 真实情况是：摘牌之后这笔钱要么按退市整理期的价格收回，要么血本无归，
// 总之不是继续持有。
//
// 价格优先级：当天价格 → 持仓里最后一次已知价 → 都取不到就跳过并告警。
// 最后一种情况宁可留着（并留下 warn）也不用 0 去平 —— 用 0 平仓等于
// 凭空抹掉一笔资产，账面上看不出来。
//
// 摘牌当天仍算在市（ListingWindow.IsListed 用 After 判断），所以真正触发
// 的是摘牌后的第一个交易日，那时通常已无行情，走 CurrentPrice 兜底。
func (e *Engine) forceCloseDelisted(
	state *BacktestState,
	pricesCache map[string]float64,
	date time.Time,
	logger zerolog.Logger,
) {
	listing := e.ListingWindows()
	if len(listing) == 0 {
		return // 没有上市日历，无从判断谁退市了
	}

	positions := state.Tracker.GetAllPositions()
	// 定序：先平哪只影响当天的现金与成交序列（P1-14）。
	for _, symbol := range sortedKeys(positions) {
		pos := positions[symbol]
		if abs(pos.Quantity) <= 1e-8 {
			continue
		}
		w, known := listing[symbol]
		if !known || w.IsListed(date) {
			continue
		}

		price, priceExists := pricesCache[symbol]
		if !priceExists || price <= 0 {
			if pos.CurrentPrice > 0 {
				price = pos.CurrentPrice
			} else {
				logger.Warn().
					Str("symbol", symbol).
					Float64("qty", pos.Quantity).
					Time("date", date).
					Msg("Skipping delisted force close: no price available (position left open)")
				continue
			}
		}

		trade, err := state.Tracker.ClosePosition(symbol, price, date)
		if err != nil {
			logger.Warn().Str("symbol", symbol).Err(err).Time("date", date).
				Msg("Failed to force close delisted position")
			continue
		}
		if trade != nil {
			logger.Info().
				Str("symbol", symbol).
				Float64("qty", trade.Quantity).
				Float64("price", trade.Price).
				Time("date", date).
				Msg("Force closed delisted position")
		}
		// 摘牌强平同样是「引擎外改持仓」（AUD-55）：票已经从 universe 里消失，
		// 不会再有信号，但那份 `TargetPosition` 账得跟着清掉。
		e.reconcileTargetPosition(state, symbol, date, logger)
	}
}

// abs returns the absolute value of x. Local copy — tracker/ and metrics/
// each have their own (S7-P2-1: tracker.go moved to tracker/ subpackage).
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
