"""NautilusTrader driver + result mapping (K13).

Drives ``BacktestNode`` (the high-level, catalog-driven path) for one RunSpec and
maps the result to the minimal field set the task asks for. Metric *alignment*
with the Go kernel's canonical ResultSpec — max drawdown, trade-level
accounting, the closed-system daily-return convention — is explicitly **not**
done here; that is K15. Fields this module cannot read directly off NT are
omitted rather than approximated, so nothing in the response is invented.
"""

from __future__ import annotations

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
from nautilus_trader.model import AccountType, BarType, Currency, NautilusDataType, OmsType

from .catalog import bar_type_str, open_catalog, resolve_instruments
from .errors import SidecarError
from .runspec import RunSpec
from .strategies import DEFAULT_TRADE_SIZE, build_demo_strategy

#: Simulated venue name. A-share symbols are stamped onto it as ``{symbol}.SIM``.
DEFAULT_VENUE = "SIM"

_STATUS_COMPLETED = "completed"
_STATUS_FAILED = "failed"

#: Statistic keys read straight off ``BacktestResult`` (nt 2.0.0rc6).
_RETURN_TOTAL_KEY = "PnL% (total)"
_SHARPE_KEY = "Sharpe Ratio (252 days)"
_SORTINO_KEY = "Sortino Ratio (252 days)"


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
    # explicit fee model on every venue. Mapping the A-share fee schedule
    # (stamp tax / transfer fee / minimum commission) is K14's job.
    venue_config = BacktestVenueConfig(
        name=venue,
        oms_type=OmsType.NETTING,
        account_type=AccountType.CASH,
        starting_balances=[f"{int(run_spec.initial_capital)} CNY"],
        base_currency=Currency.from_str("CNY"),
        fee_model=MakerTakerFeeModel(maker_rate=Decimal("0"), taker_rate=Decimal("0")),
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
                build_demo_strategy(
                    run_spec.strategy,
                    instrument_id=instrument.id,
                    # subscribe_bars takes a BarType, not its string form.
                    bar_type=BarType.from_str(bar_type_str(symbol, venue)),
                    trade_size=trade_size,
                ),
            )
        results = node.run()
        if not results:
            raise SidecarError("nautilus returned no backtest result")
        return _map_result(run_spec, run_config.id, results[0])
    except SidecarError:
        raise
    except Exception as exc:  # noqa: BLE001 — surface any NT failure as a failed run
        raise SidecarError(f"nautilus run failed: {exc}") from exc
    finally:
        try:
            node.dispose()
        except Exception:  # noqa: BLE001 — dispose must never mask the real outcome
            pass


def _map_result(run_spec: RunSpec, run_config_id: str, result: Any) -> dict[str, Any]:
    returns = dict(result.stats_returns or {})
    pnls = dict(result.stats_pnls or {})
    total_return_pct = _first_pnl(pnls, _RETURN_TOTAL_KEY)

    payload: dict[str, Any] = {
        "id": result.run_config_id or run_config_id,
        "status": _STATUS_COMPLETED,
        "strategy": run_spec.strategy,
        "start_date": run_spec.start_date,
        "end_date": run_spec.end_date,
        "initial_capital": run_spec.initial_capital,
        # NT reports percentages; ResultSpec carries a fraction.
        "total_return": None if total_return_pct is None else total_return_pct / 100.0,
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
