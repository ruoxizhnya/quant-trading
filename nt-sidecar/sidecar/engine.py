"""NautilusTrader driver + result mapping (K13 / K13-R1).

Drives ``BacktestNode`` (the high-level, catalog-driven path) for one RunSpec and
maps the result to the minimal field set the task asks for. Metric *alignment*
with the Go kernel's canonical ResultSpec — max drawdown, trade-level
accounting, the closed-system daily-return convention — is explicitly **not**
done here; that is K15. Fields this module cannot read directly off NT are
omitted rather than approximated, so nothing in the response is invented.

``total_return``口径（K13-R1 A，(b) 一步到位）
----------------------------------------------
``total_return`` **必须**与 Go 内核同口径：市值口径净值收益
``(期末总权益 − 期初本金) / 期初本金``，其中总权益 = 现金 + 持仓市值
（Go 侧权威：``pkg/backtest/metrics/performance.go`` 的
``(endValue-startValue)/startValue`` + ``pkg/backtest/tracker/tracker.go`` 的
``totalValue = cash + positionsValue``）。

**不得**再用 NT 的 ``stats_pnls[ccy]['PnL% (total)']/100``：那是「已实现+买入手
续的现金流」口径，不含持仓浮盈——价 10→20（持仓翻倍）时它仍报 −10%，与净值口径
**符号都可能相反**（K13 自审判别实验）。该原始值改名 ``nt_balance_pnl_pct`` 只作
NT 原始统计暴露在 ``nt_stats``，不再冒充 ``total_return``。

期末市值净值取自 ``BacktestNode.get_engine_portfolio(run_config_id).equity(venue)``
（NT 2.0.0rc6 实测：``{Currency: Money}``，``Money.as_double()`` 取数）。
"""

from __future__ import annotations

import logging
import math
import os
from decimal import Decimal
from typing import Any

import pandas as pd
from nautilus_trader.backtest import BacktestNode
from nautilus_trader.common import LogLevel
from nautilus_trader.config import (
    BacktestDataConfig,
    BacktestEngineConfig,
    BacktestRunConfig,
    BacktestVenueConfig,
    LoggerConfig,
)
from nautilus_trader.execution import MakerTakerFeeModel
from nautilus_trader.model import (
    AccountType,
    BarType,
    Currency,
    NautilusDataType,
    OmsType,
    Venue,
)

from . import ashare
from . import native as native_strategies
from .catalog import bar_type_str, open_catalog, resolve_instruments
from .errors import SidecarError
from .markets import (
    DEFAULT_MARKET,
    EXECUTION_NATIVE,
    FEE_MODEL_ASHARE,
    MarketProfile,
    resolve_market,
)
from .runspec import RunSpec
from .strategies import (
    DEFAULT_TRADE_SIZE,
    WEIGHT_REPLAY,
    build_strategy,
)

#: Simulated venue name. A-share symbols are stamped onto it as ``{symbol}.SIM``.
DEFAULT_VENUE = "SIM"

_STATUS_COMPLETED = "completed"
_STATUS_FAILED = "failed"

#: NT's cash-flow PnL% (excludes open-position mark-to-market). Kept for
#: provenance only — never mapped to ``total_return`` (see module docstring).
_BALANCE_PNL_PCT_KEY = "PnL% (total)"
_SHARPE_KEY = "Sharpe Ratio (252 days)"
_SORTINO_KEY = "Sortino Ratio (252 days)"

log = logging.getLogger("nt-sidecar")


def _fee_model_for(profile: MarketProfile, strategy: str):
    """Pick the venue fee model (K14 / D7).

    ``CN`` (A-share line, unchanged): the K14 weight-replay adapter charges the
    A-share schedule (stamp tax / transfer fee / minimum commission,
    :mod:`sidecar.ashare`); the K13 buy-and-hold demo keeps the explicit
    zero-fee model — its published evidence numbers (total_return −0.1347 /
    Sharpe 1.4267) are zero-fee and must not move.

    ``US`` (foreign line, D7): NT's **own** fee model, not a hand-rolled
    A-share-style schedule. It is configured at an explicit zero rate here
    because the sample run has no fee schedule to speak of — the real US equity
    schedule arrives with the market data connector (D9 follow-up). Zero is
    stated, not invented.
    """
    if profile.fee_model != FEE_MODEL_ASHARE:
        return MakerTakerFeeModel(maker_rate=Decimal("0"), taker_rate=Decimal("0"))
    if strategy == WEIGHT_REPLAY:
        return ashare.make_fee_model()
    return MakerTakerFeeModel(maker_rate=Decimal("0"), taker_rate=Decimal("0"))


def run_backtest(
    run_spec: RunSpec,
    *,
    catalog_path: str,
    venue: str = DEFAULT_VENUE,
    trade_size: Decimal = DEFAULT_TRADE_SIZE,
    market: str = DEFAULT_MARKET,
) -> dict[str, Any]:
    """Execute one RunSpec and return the minimal ResultSpec-shaped payload.

    ``market`` selects the **market profile** (D7): it is a property of the
    sidecar deployment (``python -m sidecar --market US``), *not* of the
    RunSpec — the frozen RunSpec contract is untouched by D7 (the market never
    travels on the wire). An unregistered market is refused by
    :func:`sidecar.markets.resolve_market` before anything else happens.
    """
    profile = resolve_market(market)
    catalog = open_catalog(catalog_path)
    instruments = resolve_instruments(catalog, run_spec.stock_pool, venue)

    start_ns = _date_to_ns(run_spec.start_date)
    end_ns = _date_to_ns(run_spec.end_date)

    # K13 runs with an explicit zero-fee model: NautilusTrader requires an
    # explicit fee model on every venue. K14's weight-replay adapter charges the
    # A-share schedule instead; D7's native markets use NT's own model
    # (see ``_fee_model_for``).
    venue_config = BacktestVenueConfig(
        name=venue,
        oms_type=OmsType.NETTING,
        account_type=AccountType.CASH,
        starting_balances=[f"{int(run_spec.initial_capital)} {profile.currency}"],
        base_currency=Currency.from_str(profile.currency),
        fee_model=_fee_model_for(profile, run_spec.strategy),
    )

    data_configs = [
        BacktestDataConfig(
            data_type=NautilusDataType.Bar,
            catalog_path=str(catalog_path),
            instrument_id=instrument.id,
            bar_types=[bar_type_str(symbol, venue)],
            start_time=start_ns,
            end_time=end_ns,
        )
        for symbol, instrument in instruments.items()
    ]

    run_config = BacktestRunConfig(
        venues=[venue_config],
        data=data_configs,
        engine=BacktestEngineConfig(logging=LoggerConfig(stdout_level=LogLevel.ERROR)),
        # Keep engine state so post-run reports stay inspectable (rc6 default drops it).
        dispose_on_completion=False,
    )

    node = BacktestNode(configs=[run_config])
    try:
        node.build()
        # K14-R1 A · 权重是「组合总权重」，而 NT 为每个 symbol 各建一个策略
        # 实例。若每实例都拿**全额** initial_capital，N 个标的最多承诺
        # N × weight × 本金 ⇒ 组合级资金超配（2 标的实测：一条腿被现金不足
        # 拒单，run 仍报 completed + 一个像真的 total_return）。
        # 最小正确修：把本金按标的数分摊，使 Σ 名义额 ≤ 本金（各 weight ≤ 1）。
        # 组合级统一预算（跨实例共享可变状态）会引入非确定性，留待 K15 按需评估。
        capital_per_symbol = run_spec.initial_capital / len(instruments)
        for symbol, instrument in instruments.items():
            node.add_strategy(
                run_config.id,
                _build_strategy_for(
                    profile,
                    run_spec,
                    instrument_id=instrument.id,
                    # subscribe_bars takes a BarType, not its string form.
                    bar_type=BarType.from_str(bar_type_str(symbol, venue)),
                    trade_size=trade_size,
                    capital_per_symbol=capital_per_symbol,
                ),
            )
        results = node.run()
        if not results:
            raise SidecarError("nautilus returned no backtest result")
        # The portfolio must be read *before* dispose() (the finally block): rc6
        # drops engine state on completion, so ``dispose_on_completion=False`` is
        # required for this read to work (set above).
        portfolio = _safe_portfolio(node, run_config.id)
        # K15 · the K14/K13 mapping returns a minimal metric set; the dual-run
        # alignment needs the full data plane (per-fill trade list + per-day
        # cash events), which is only extractable while the engine is alive.
        # All three are read here, inside the same pre-dispose window.
        fills_report = node.generate_fills_report(run_config.id)
        account_report = node.generate_account_report(
            run_config.id, venue=Venue(venue)
        )
        closes = _catalog_closes(catalog_path, run_spec.stock_pool, venue)
        return _map_result(
            run_spec,
            run_config.id,
            results[0],
            portfolio=portfolio,
            venue=venue,
            fills_report=fills_report,
            account_report=account_report,
            closes=closes,
        )
    except SidecarError:
        raise
    except Exception as exc:  # noqa: BLE001 — surface any NT failure as a failed run
        raise SidecarError(f"nautilus run failed: {exc}") from exc
    finally:
        try:
            node.dispose()
        except Exception:  # noqa: BLE001 — dispose must never mask the real outcome
            pass


def _build_strategy_for(
    profile: MarketProfile,
    run_spec: RunSpec,
    *,
    instrument_id: Any,
    bar_type: BarType,
    trade_size: Decimal,
    capital_per_symbol: float,
) -> Any:
    """按市场档案的执行模式造策略（D7 的分派点）。

    ``weight_replay``（CN，逐字不变）：K14 适配器，参数与 K13–K15 时期完全
    一致——``d_max`` / ``signals`` / 窗口日期照旧传给 ``build_strategy``。

    ``native``（D7 新增）：NT 原生策略，**不传** A 股专属参数（``d_max``
    参与率上限、权重信号流都是 A 股适配器的东西），国外线不吃这一套。
    """
    if profile.execution_mode == EXECUTION_NATIVE:
        return native_strategies.build_native_strategy(
            run_spec.strategy,
            instrument_id=instrument_id,
            bar_type=bar_type,
            trade_size=trade_size,
        )
    return build_strategy(
        run_spec.strategy,
        instrument_id=instrument_id,
        bar_type=bar_type,
        trade_size=trade_size,
        start_date=run_spec.start_date,
        end_date=run_spec.end_date,
        initial_capital=capital_per_symbol,
        d_max=run_spec.d_max,
        # K15 · caller-supplied target-weight stream (empty → the K14 built-in
        # sample stream, unchanged).
        signal_rows=run_spec.signals,
    )


def _safe_portfolio(node: Any, run_config_id: str) -> Any | None:
    """Best-effort read of the engine portfolio; ``None`` on any failure."""
    try:
        return node.get_engine_portfolio(run_config_id)
    except Exception as exc:  # noqa: BLE001 — never let this mask the run outcome
        log.warning("could not read engine portfolio: %s", exc)
        return None


def _end_equity(portfolio: Any, venue: str) -> float | None:
    """Mark-to-market account equity at the end of the run.

    ``Portfolio.equity(venue)`` returns ``{Currency: Money}`` (NT 2.0.0rc6),
    i.e. cash + open-position market value — the same notion the Go kernel puts
    in ``PortfolioValue.TotalValue`` (tracker.go:948).
    """
    if portfolio is None:
        return None
    try:
        equity = portfolio.equity(venue=Venue(venue))
    except Exception as exc:  # noqa: BLE001 — best effort, never fabricate a number
        log.warning("could not read portfolio equity for venue %s: %s", venue, exc)
        return None
    if not equity:
        return None
    try:
        return float(sum(money.as_double() for money in equity.values()))
    except Exception as exc:  # noqa: BLE001
        log.warning("could not sum portfolio equity: %s", exc)
        return None


def _map_result(
    run_spec: RunSpec,
    run_config_id: str,
    result: Any,
    *,
    portfolio: Any = None,
    venue: str = DEFAULT_VENUE,
    fills_report: Any = None,
    account_report: Any = None,
    closes: dict[str, Any] | None = None,
) -> dict[str, Any]:
    returns = dict(result.stats_returns or {})
    pnls = dict(result.stats_pnls or {})
    balance_pnl_pct = _first_pnl(pnls, _BALANCE_PNL_PCT_KEY)

    # K13-R1 A · 市值口径净值收益（与 Go 内核同口径，见模块 docstring）。
    end_equity = _end_equity(portfolio, venue)
    total_return: float | None = None
    if end_equity is not None and run_spec.initial_capital > 0:
        total_return = (end_equity - run_spec.initial_capital) / run_spec.initial_capital

    # K13-R1-R1 A (b) · 源头兜底：市值口径净值读不到 ⇒ 该 run 是 **failed**，
    # 不是 ``completed`` + 一个像真的 0。把「读不到的口径」呈现成一个像真的
    # 数字，正是 K13-R1 A 要治的病；这里让「缺字段」在源头不可能发生。
    status = _STATUS_COMPLETED
    error = ""
    if total_return is None:
        status = _STATUS_FAILED
        error = (
            "could not read end-of-run mark-to-market equity, so total_return "
            "cannot be reported (rejected, not folded to 0)"
        )

    # K15 · full data plane for the dual-run alignment. The equity curve is
    # synthesised from the same primitives the Go tracker uses —
    # equity(d) = cash(d) + Σ qty(sym,d) × close(sym,d) (tracker.go 的
    # totalValue = cash + positionsValue) — so the Go diff tool can compare
    # like with like and attribute every difference to the execution
    # mechanism, never to a metric-formula drift.
    equity_curve: list[dict[str, Any]] = []
    trades: list[dict[str, Any]] = []
    if status == _STATUS_COMPLETED:
        equity_curve = synth_equity_curve(
            run_spec.initial_capital,
            account_report,
            fills_report,
            closes or {},
            venue=venue,
            start_date=run_spec.start_date,
            end_date=run_spec.end_date,
        )
        trades = map_trades(fills_report, venue=venue)
        # A completed run must carry a usable equity curve; a degenerate one
        # (no account events at all) is a mapping failure, not a flat run.
        if not equity_curve:
            status = _STATUS_FAILED
            error = (
                "could not synthesise the daily equity curve from account/fill "
                "reports (rejected, not folded to an empty series)"
            )
            equity_curve = []
            trades = []

    payload: dict[str, Any] = {
        "id": result.run_config_id or run_config_id,
        "status": status,
        "strategy": run_spec.strategy,
        "start_date": run_spec.start_date,
        "end_date": run_spec.end_date,
        "initial_capital": run_spec.initial_capital,
        # Fraction, market-value net return (K13-R1 A, (b)). ``None`` when the
        # equity could not be read — never the cash-flow PnL% masquerading as it.
        "total_return": total_return,
        "sharpe_ratio": _finite(returns.get(_SHARPE_KEY)),
        "sortino_ratio": _finite(returns.get(_SORTINO_KEY)),
        "total_orders": int(result.total_orders),
        "total_positions": int(result.total_positions),
        "iterations": int(result.iterations),
        # K15 · per-day mark-to-market series and the per-fill trade list.
        "equity_curve": equity_curve,
        "trades": trades,
        # Carried only when the run is failed (OBS-01: failed is an outcome).
        "error": error,
        # Raw NT statistics, sanitised for JSON. Carried so K15 can align every
        # metric without re-running, and so the link is provably NT's own output.
        "nt_stats": {
            "general": _sanitise(dict(result.stats_general or {})),
            "returns": _sanitise(returns),
            "pnls": _sanitise(pnls),
            # The cash-flow PnL% kept as raw provenance only (fraction form).
            "nt_balance_pnl_pct": None if balance_pnl_pct is None else balance_pnl_pct / 100.0,
            "nt_end_equity": end_equity,
        },
    }
    return payload


def _date_to_ns(date_str: str) -> int:
    """Midnight UTC of an A-share trading date, in nanoseconds.

    Daily A-share bars carry no intraday timestamp; the convention here is the
    UTC midnight of the trade date (see design doc §8, item 3).
    """
    return int(pd.Timestamp(date_str, tz="UTC").value)


def _first_pnl(pnls: dict[str, Any], key: str) -> float | None:
    for stats in pnls.values():
        if isinstance(stats, dict) and key in stats:
            return _finite(stats[key])
    return None


def _finite(value: Any) -> float | None:
    """Return a JSON-safe float, mapping NaN/Inf to None (strict JSON has no NaN)."""
    if value is None:
        return None
    try:
        number = float(value)
    except (TypeError, ValueError):
        return None
    return number if math.isfinite(number) else None


def _sanitise(stats: dict[str, Any]) -> dict[str, Any]:
    """Recursively replace non-finite floats so the payload is strict JSON."""
    clean: dict[str, Any] = {}
    for key, value in stats.items():
        if isinstance(value, dict):
            clean[key] = _sanitise(value)
        elif isinstance(value, float):
            clean[key] = _finite(value)
        else:
            clean[key] = value
    return clean


# ── K15 · data-plane mapping (dual-run alignment) ────────────────────────────


def _catalog_closes(catalog_path: str, stock_pool: tuple[str, ...], venue: str) -> dict[str, Any]:
    """Per-symbol ``{date → close}`` series read from the Parquet catalog.

    The equity synthesis marks positions to the **same close series the NT run
    consumed**, so the mark price can never be a second, drifting source. The
    parquet files are read directly (pyarrow) — the same files the NT data
    loader consumes — because the catalog's object API returns typed Bar
    objects that are awkward to flatten.
    """
    import glob
    import os

    import pyarrow.parquet as pq

    from .catalog import bar_type_str

    closes: dict[str, Any] = {}
    for symbol in stock_pool:
        pattern = os.path.join(
            str(catalog_path), "data", "bars", f"{bar_type_str(symbol, venue)}", "*.parquet"
        )
        files = sorted(glob.glob(pattern))
        if not files:
            log.warning("no parquet files for %s under %s", symbol, pattern)
            continue
        frames = [pq.read_table(f).to_pandas() for f in files]
        frame = pd.concat(frames, ignore_index=True)
        ts = pd.to_datetime(frame["ts_event"], utc=True)
        frame = frame.assign(_date=ts.dt.date).sort_values("_date")
        closes[symbol] = frame.groupby("_date")["close"].last()
    return closes


def _symbol_of(value: Any, venue: str) -> str:
    """InstrumentId or wire string → bare symbol (``"000001.SZ.SIM" → "000001.SZ"``).

    The fills report's ``instrument_id`` column round-trips through pyarrow as
    a plain ``"symbol.venue"`` string, so both object and string forms end up
    here; the venue suffix is always stripped to match the catalog's bare
    symbol keys.
    """
    text = str(value)
    suffix = f".{venue}"
    if text.endswith(suffix):
        text = text[: -len(suffix)]
    return text


def synth_equity_curve(
    initial_capital: float,
    account_report: Any,
    fills_report: Any,
    closes: dict[str, Any],
    *,
    venue: str = DEFAULT_VENUE,
    start_date: str = "",
    end_date: str = "",
) -> list[dict[str, Any]]:
    """Synthesise the daily mark-to-market series the Go tracker records.

    ``equity(d) = cash(d) + Σ_sym qty(sym,d) × close(sym,d)`` — the exact
    ``totalValue = cash + positionsValue`` notion of ``tracker.go``. Dates are
    the union of the catalog bar dates (the shared trading calendar) clipped to
    the run window; on a day with no account event the cash carries forward
    (no trade → no cash move), and a symbol's close carries forward likewise
    (suspension → value frozen).

    Returns ``[]`` when either input report is missing — the caller turns that
    into a ``failed`` run, never into an empty-but-valid curve.
    """
    if account_report is None or account_report.empty:
        return []
    if not closes:
        return []

    calendar = sorted({d for series in closes.values() for d in series.index})
    if start_date:
        start = pd.Timestamp(start_date, tz="UTC").date()
        calendar = [d for d in calendar if d >= start]
    if end_date:
        end = pd.Timestamp(end_date, tz="UTC").date()
        calendar = [d for d in calendar if d <= end]
    if not calendar:
        return []

    # cash(d): last account event per day, carried forward; starts at capital.
    cash_by_day: dict[Any, float] = {}
    cash = float(initial_capital)
    events = account_report.copy()
    events["_date"] = pd.to_datetime(events.index, utc=True).date
    for day, group in events.groupby("_date", sort=True):
        cash = float(group["total"].iloc[-1])
        cash_by_day[day] = cash

    # qty(sym, d): signed fill accumulation per symbol.
    qty_events: dict[str, list[tuple[Any, float]]] = {}
    if fills_report is not None and not fills_report.empty:
        fills = fills_report.copy()
        fills["_date"] = pd.to_datetime(fills["ts_event"], utc=True).dt.date
        for _, row in fills.iterrows():
            side = str(row["order_side"]).upper()
            signed = float(row["last_qty"]) * (1.0 if side == "BUY" else -1.0)
            qty_events.setdefault(_symbol_of(row["instrument_id"], venue), []).append(
                (row["_date"], signed)
            )
    for events_list in qty_events.values():
        events_list.sort(key=lambda pair: pair[0])

    def _pos_qty(symbol: str, day: Any) -> float:
        return sum(signed for fill_day, signed in qty_events.get(symbol, []) if fill_day <= day)

    def _close(symbol: str, day: Any, last_close: dict[str, float]) -> float | None:
        series = closes.get(symbol)
        if series is None:
            return None
        # Carry the last known close forward across suspension days.
        eligible = series[series.index <= day]
        if len(eligible) == 0:
            return None
        return float(eligible.iloc[-1])

    out: list[dict[str, Any]] = []
    for day in calendar:
        day_cash = cash_by_day.get(day, cash_by_day.get(_last_key_before(cash_by_day, day)))
        if day_cash is None:
            # Days before the first account event: the opening balance applies.
            day_cash = float(initial_capital)
        positions = 0.0
        for symbol, series in closes.items():
            qty = _pos_qty(symbol, day)
            if qty == 0:
                continue
            px = _close(symbol, day, {})
            if px is None:
                px = 0.0
            positions += qty * px
        out.append(
            {
                "date": day.isoformat(),
                "equity": round(day_cash + positions, 2),
                "cash": round(float(day_cash), 2),
                "positions": round(positions, 2),
            }
        )
    return out


def _last_key_before(daily: dict[Any, float], day: Any) -> Any | None:
    """The latest day in ``daily`` strictly before ``day`` (dict is date-keyed)."""
    prior = [d for d in daily if d < day]
    return max(prior) if prior else None


def map_trades(fills_report: Any, *, venue: str = DEFAULT_VENUE) -> list[dict[str, Any]]:
    """Map the NT per-fill report onto the wire trade rows.

    NT's ``AShareFeeModel`` returns the **total** A-share fee as one ``Money``
    (commission + transfer fee + sell-side stamp tax floored together), so the
    wire row carries it as ``commission`` with zero-valued components — the Go
    diff tool compares ``commission + transfer_fee + stamp_tax`` on its side
    against this total, keeping one number per side.
    """
    if fills_report is None or fills_report.empty:
        return []
    rows: list[dict[str, Any]] = []
    fills = fills_report.sort_values("ts_event")
    for _, row in fills.iterrows():
        rows.append(
            {
                "symbol": _symbol_of(row["instrument_id"], venue),
                "side": str(row["order_side"]).upper(),
                "qty": float(row["last_qty"]),
                "price": float(row["last_px"]),
                "commission": _parse_money(row["commission"]),
                "transfer_fee": 0.0,
                "stamp_tax": 0.0,
                "ts": pd.Timestamp(row["ts_event"]).isoformat(),
            }
        )
    return rows


def _parse_money(value: Any) -> float:
    """``"12.34 CNY"`` → ``12.34``; non-finite / malformed → 0.0 (never crash a report)."""
    if value is None:
        return 0.0
    try:
        text = str(value).split(" ")[0]
        number = float(text)
    except (ValueError, IndexError):
        return 0.0
    return number if math.isfinite(number) else 0.0
