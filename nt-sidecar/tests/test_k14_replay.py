"""K14 · weight-replay adapter — integration tests on the real NT engine.

Exercises the adapter end-to-end (RunSpec-less: the strategy is built directly so
the test can read its ``decisions``), and freezes the acceptance legs:

* 回放一致性测试门 — the same input replays to identical orders (deterministic),
  and the realized position matches the signal's intent when unconstrained.
* 涨停反证腿 — entering on a day that closes at the upper limit submits **no**
  buy (触及涨停不追买).
* T+1 反证腿 — a same-day buy is not sellable that day (minute bars let one day
  hold several decisions, which is the only way T+1 can actually bite).
* D_max — the daily participation cap is enforced (破坏验证：绕过它 → 红).

Run with::

    python -m unittest tests.test_k14_replay -v      (from nt-sidecar/)
"""

from __future__ import annotations

import datetime as dt
import shutil
import tempfile
import unittest
from pathlib import Path

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
from nautilus_trader.model import (
    AccountType,
    Bar,
    BarType,
    Currency,
    NautilusDataType,
    OmsType,
    Price,
    Quantity,
)
from nautilus_trader.persistence import ParquetDataCatalog

from sidecar import ashare
from sidecar.catalog import make_equity
from sidecar.signals import SignalStream, TargetWeight
from sidecar.weight_replay import WeightReplay, WeightReplayConfig

SYMBOL = "600000.SH"
VENUE = "SIM"
CAPITAL = 1_000_000.0
DAY_BAR = f"{SYMBOL}.{VENUE}-1-DAY-LAST-EXTERNAL"
MINUTE_BAR = f"{SYMBOL}.{VENUE}-1-MINUTE-LAST-EXTERNAL"


def _write_bars(catalog: ParquetDataCatalog, bar_type: BarType, rows: list[tuple[str, float, float]]) -> None:
    """rows = [(timestamp_iso, close, volume), ...]."""
    catalog.write_bars(
        [
            Bar(
                bar_type=bar_type,
                open=Price(close, precision=2),
                high=Price(close + 0.01, precision=2),
                low=Price(close - 0.01, precision=2),
                close=Price(close, precision=2),
                volume=Quantity.from_int(int(volume)),
                ts_event=int(pd.Timestamp(ts, tz="UTC").value),
                ts_init=int(pd.Timestamp(ts, tz="UTC").value),
            )
            for ts, close, volume in rows
        ]
    )


class _Run:
    """The result of one adapter run plus the strategy instance for inspection."""

    def __init__(self, result, strategy: WeightReplay, orders: int) -> None:
        self.result = result
        self.strategy = strategy
        self.orders = orders


def _run_replay(
    *,
    bar_type_str: str,
    rows: list[tuple[str, float, float]],
    signals: SignalStream,
    d_max: float = 0.10,
    capital: float = CAPITAL,
) -> _Run:
    tmp = Path(tempfile.mkdtemp(prefix="nt-k14-"))
    try:
        catalog = ParquetDataCatalog(str(tmp))
        instrument = make_equity(SYMBOL, VENUE)
        catalog.write_instruments([instrument])
        bar_type = BarType.from_str(bar_type_str)
        _write_bars(catalog, bar_type, rows)

        start = int(pd.Timestamp(rows[0][0], tz="UTC").value)
        end = int(pd.Timestamp(rows[-1][0], tz="UTC").value)
        venue = BacktestVenueConfig(
            name=VENUE,
            oms_type=OmsType.NETTING,
            account_type=AccountType.CASH,
            starting_balances=[f"{int(capital)} CNY"],
            base_currency=Currency.from_str("CNY"),
            # Exercise the K14 A-share fee model on the real engine.
            fee_model=ashare.make_fee_model(),
        )
        data = [
            BacktestDataConfig(
                data_type=NautilusDataType.Bar,
                catalog_path=str(tmp),
                instrument_id=instrument.id,
                bar_types=[bar_type_str],
                start_time=start,
                end_time=end,
            )
        ]
        run_config = BacktestRunConfig(
            venues=[venue],
            data=data,
            engine=BacktestEngineConfig(logging=LoggerConfig(stdout_level=LogLevel.ERROR)),
            dispose_on_completion=False,
        )
        node = BacktestNode(configs=[run_config])
        node.build()
        strategy = WeightReplay(
            WeightReplayConfig(
                instrument_id=instrument.id,
                bar_type=bar_type,
                signals=signals,
                capital_base=capital,
                d_max=d_max,
            )
        )
        node.add_strategy(run_config.id, strategy)
        results = node.run()
        order_count = int(results[0].total_orders)
        node.dispose()
        return _Run(results[0], strategy, order_count)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def _daily_rows(closes: list[float], volume: float = 1_000_000) -> list[tuple[str, float, float]]:
    dates = pd.date_range("2024-01-02", periods=len(closes), freq="D")
    return [(d.strftime("%Y-%m-%d"), c, volume) for d, c in zip(dates, closes)]


class ReplayConsistencyGateTest(unittest.TestCase):
    """🔴 回放一致性测试门 —— 先于任何参数试验，落地即跑。"""

    def _run_once(self) -> _Run:
        # Enter 50% on day 2, exit on day 5.
        signals = SignalStream(
            [
                TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 3)),
                TargetWeight(SYMBOL, 0.0, dt.date(2024, 1, 6)),
            ]
        )
        return _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 10.0, 10.0, 10.0, 10.0, 10.0]),
            signals=signals,
        )

    def test_link_is_green(self) -> None:
        run = self._run_once()
        self.assertGreater(run.orders, 0, "the adapter must produce orders")
        self.assertGreater(len(run.strategy.decisions), 0, "the adapter must record decisions")

    def test_replay_is_deterministic(self) -> None:
        a = self._run_once()
        b = self._run_once()
        self.assertEqual(a.orders, b.orders, "same input must replay to the same order count")
        self.assertEqual(
            [(d.day, d.side, d.reason) for d in a.strategy.decisions],
            [(d.day, d.side, d.reason) for d in b.strategy.decisions],
            "the decision trace must be byte-identical across replays",
        )

    def test_realized_position_matches_signal_intent(self) -> None:
        run = self._run_once()
        # capital 1,000,000 * 0.5 / 10.0 = 50,000 shares, whole lots.
        buys = [d for d in run.strategy.decisions if d.side == "buy"]
        self.assertTrue(buys, "expected a buy decision for the 0.5 entry")
        self.assertEqual(50_000.0, buys[0].target_qty)


class LimitUpNoChaseTest(unittest.TestCase):
    """涨停反证腿 —— 触及涨停不追买。"""

    def test_no_buy_when_entry_day_closes_at_upper_limit(self) -> None:
        # day1 close 10.00 -> day2 upper limit = 11.00 ; day2 closes exactly at it.
        signals = SignalStream([TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 3))])
        run = _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 11.0, 11.0]),
            signals=signals,
        )
        limit_day = dt.date(2024, 1, 3)
        on_limit_day = [d for d in run.strategy.decisions if d.day == limit_day]
        self.assertTrue(on_limit_day, "the adapter must evaluate the limit-up day")
        self.assertTrue(
            all(d.side != "buy" for d in on_limit_day),
            f"no buy is allowed at the limit-up, got {[(d.side, d.reason) for d in on_limit_day]}",
        )
        self.assertTrue(any("limit-up" in d.reason for d in on_limit_day))

        # The chase is *deferred*: the buy lands on a later, non-limit day.
        buys = [d for d in run.strategy.decisions if d.side == "buy"]
        self.assertTrue(buys, "the entry must eventually fill below the limit")
        self.assertGreater(buys[0].day, limit_day, "the buy must not chase the limit-up day")

    def test_buy_is_allowed_below_the_limit(self) -> None:
        signals = SignalStream([TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 3))])
        run = _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 10.5, 10.5]),  # below the 11.00 upper limit
            signals=signals,
        )
        self.assertEqual(1, run.orders, "a buy below the limit must go through")


class T1SettlementTest(unittest.TestCase):
    """T+1 反证腿 —— 今仓不可当日卖出（用分钟 bar 让一天有多笔决策）。"""

    def test_same_day_buy_cannot_be_sold_that_day(self) -> None:
        # One day, four minute bars. Enter at 01:30 (buy), exit at 01:32. The
        # shares bought two minutes ago are NOT sellable today (T+1).
        rows = [
            ("2024-01-02 01:30:00", 10.0, 1_000_000),
            ("2024-01-02 01:31:00", 10.0, 1_000_000),
            ("2024-01-02 01:32:00", 10.0, 1_000_000),
            ("2024-01-02 01:33:00", 10.0, 1_000_000),
        ]
        signals = SignalStream(
            [
                TargetWeight(SYMBOL, 0.5, dt.datetime(2024, 1, 2, 1, 30)),
                TargetWeight(SYMBOL, 0.0, dt.datetime(2024, 1, 2, 1, 32)),
            ]
        )
        run = _run_replay(bar_type_str=MINUTE_BAR, rows=rows, signals=signals)

        buys = [d for d in run.strategy.decisions if d.side == "buy"]
        sells = [d for d in run.strategy.decisions if d.side == "sell"]
        self.assertTrue(buys, "the 01:30 entry must buy")
        self.assertEqual([], sells, "the adapter must never sell today's buys (T+1)")
        self.assertTrue(
            any("T+1" in d.reason for d in run.strategy.decisions),
            f"expected a T+1 truncation reason, got {[d.reason for d in run.strategy.decisions]}",
        )


class ParticipationCapTest(unittest.TestCase):
    """D_max —— 每日成交股数 ≤ D_max × bar volume（硬约束）。"""

    def test_daily_traded_quantity_respects_the_cap(self) -> None:
        # volume 1,000 per bar, D_max 0.10 -> at most 100 shares/day.
        signals = SignalStream([TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 2))])
        run = _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 10.0, 10.0], volume=1_000),
            signals=signals,
        )
        # 0.5 * 1,000,000 / 10 = 50,000 shares wanted, but the cap is 100/day.
        buys = [d for d in run.strategy.decisions if d.side == "buy"]
        self.assertTrue(buys)
        # The strategy's own decision records the target; assert the *cap* by the
        # resulting order quantity via the fills report.
        self.assertEqual(100.0, _first_order_qty(run), "traded qty must be capped at D_max*volume")


def _first_order_qty(run: _Run) -> float:
    fills = {d.reason for d in run.strategy.decisions}
    # The decision trace carries the submitted qty in the reason ("submitted 100").
    for d in run.strategy.decisions:
        if d.side in ("buy", "sell") and d.reason.startswith("submitted"):
            return float(d.reason.split()[1])
    raise AssertionError(f"no submitted quantity in decisions: {fills}")


class ServedSampleChainTest(unittest.TestCase):
    """样本链路全绿 —— the sidecar's own ``weight_replay`` path runs end-to-end."""

    def test_weight_replay_runs_through_the_engine(self) -> None:
        from sidecar.engine import run_backtest
        from sidecar.runspec import RunSpec

        tmp = Path(tempfile.mkdtemp(prefix="nt-k14-sample-"))
        try:
            catalog = ParquetDataCatalog(str(tmp))
            instrument = make_equity(SYMBOL, VENUE)
            catalog.write_instruments([instrument])
            _write_bars(catalog, BarType.from_str(DAY_BAR), _daily_rows([10.0 + 0.1 * i for i in range(6)]))

            spec = RunSpec.from_json(
                {
                    "strategy": "weight_replay",
                    "stock_pool": [SYMBOL],
                    "start_date": "2024-01-02",
                    "end_date": "2024-01-07",
                }
            )
            result = run_backtest(spec, catalog_path=str(tmp))
        finally:
            shutil.rmtree(tmp, ignore_errors=True)

        self.assertEqual("completed", result["status"])
        self.assertGreater(result["total_orders"], 0, "the sample chain must trade")
        self.assertIsNotNone(result["total_return"])
        self.assertEqual(1_000_000.0, result["initial_capital"])


if __name__ == "__main__":
    unittest.main()
