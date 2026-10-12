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
from .catalog import bar_type_str, open_catalog, resolve_instruments
from .errors import SidecarError
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


def _fee_model_for(strategy: str):
    """Pick the venue fee model for a strategy (K14).

    The K14 weight-replay adapter charges the **A-share schedule** (stamp tax /
    transfer fee / minimum commission, :mod:`sidecar.ashare`). The K13
    buy-and-hold demo keeps the explicit **zero-fee** model — its published
    evidence numbers (total_return −0.1347 / Sharpe 1.4267) are zero-fee and
    must not move.
    """
    if strategy == WEIGHT_REPLAY:
        return ashare.make_fee_model()
    return MakerTakerFeeModel(maker_rate=Decimal("0"), taker_rate=Decimal("0"))


def run_backtest(
    run_spec: RunSpec,
    *,
    catalog_path: str,
    venue: str = DEFAULT_VENUE,
    trade_size: Decimal = DEFAULT_TRADE_SIZE,
) -> dict[str, Any]:
    """Execute one RunSpec and return the minimal ResultSpec-shaped payload."""
    catalog = open_catalog(catalog_path)
    instruments = resolve_instruments(catalog, run_spec.stock_pool, venue)

    start_ns = _date_to_ns(run_spec.start_date)
    end_ns = _date_to_ns(run_spec.end_date)

    # K13 runs with an explicit zero-fee model: NautilusTrader requires an
    # explicit fee model on every venue. K14's weight-replay adapter charges the
    # A-share schedule instead (see ``_fee_model_for``).
    venue_config = BacktestVenueConfig(
        name=venue,
        oms_type=OmsType.NETTING,
        account_type=AccountType.CASH,
        starting_balances=[f"{int(run_spec.initial_capital)} CNY"],
        base_currency=Currency.from_str("CNY"),
        fee_model=_fee_model_for(run_spec.strategy),
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
        for symbol, instrument in instruments.items():
            node.add_strategy(
                run_config.id,
                build_strategy(
                    run_spec.strategy,
                    instrument_id=instrument.id,
                    # subscribe_bars takes a BarType, not its string form.
                    bar_type=BarType.from_str(bar_type_str(symbol, venue)),
                    trade_size=trade_size,
                    start_date=run_spec.start_date,
                    end_date=run_spec.end_date,
                    initial_capital=run_spec.initial_capital,
                    d_max=run_spec.d_max,
                ),
            )
        results = node.run()
        if not results:
            raise SidecarError("nautilus returned no backtest result")
        # The portfolio must be read *before* dispose() (the finally block): rc6
        # drops engine state on completion, so ``dispose_on_completion=False`` is
        # required for this read to work (set above).
        portfolio = _safe_portfolio(node, run_config.id)
        return _map_result(
            run_spec,
            run_config.id,
            results[0],
            portfolio=portfolio,
            venue=venue,
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
) -> dict[str, Any]:
    returns = dict(result.stats_returns or {})
    pnls = dict(result.stats_pnls or {})
    balance_pnl_pct = _first_pnl(pnls, _BALANCE_PNL_PCT_KEY)

    # K13-R1 A · 市值口径净值收益（与 Go 内核同口径，见模块 docstring）。
    end_equity = _end_equity(portfolio, venue)
    total_return: float | None = None
    if end_equity is not None and run_spec.initial_capital > 0:
        total_return = (end_equity - run_spec.initial_capital) / run_spec.initial_capital

    payload: dict[str, Any] = {
        "id": result.run_config_id or run_config_id,
        "status": _STATUS_COMPLETED,
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
