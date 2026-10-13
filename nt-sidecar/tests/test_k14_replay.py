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
    capital_base: float | None = None,
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
                # K14-R1 B needs the venue cash and the intended notional to be
                # independently controllable to force a real rejection.
                capital_base=capital if capital_base is None else capital_base,
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

    def test_cap_quantity_is_re_rounded_to_a_whole_lot(self) -> None:
        """K14-R1 C · cap=105 场景：买入须再取整为整手（100），不得提交 105。"""
        # volume 1,050 × D_max 0.10 -> floor(105) = 105, which is not lot-aligned.
        signals = SignalStream([TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 2))])
        run = _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 10.0], volume=1_050),
            signals=signals,
        )
        self.assertEqual(
            100.0,
            _first_order_qty(run),
            "a non-lot participation cap must be rounded down to a whole lot",
        )


def _first_order_qty(run: _Run) -> float:
    # K14-R1 D · read the structured ``submitted`` field, not a parsed reason string.
    for d in run.strategy.decisions:
        if d.submitted > 0:
            return d.submitted
    raise AssertionError(
        f"no submitted quantity in decisions: {[d.reason for d in run.strategy.decisions]}"
    )


class TradedBudgetFedByFillsTest(unittest.TestCase):
    """K14-R1-R1 R1 · 成交必须喂当日参与率预算（正证据腿）。"""

    def test_fills_consume_the_daily_participation_budget(self) -> None:
        # 同一日两根分钟 bar：volume 1,000/bar、d_max 0.10 ⇒ cap = floor(100) = 100。
        # 首根提交 100 并成交；第二根的预算必须已被成交消耗。若 fill 未喂预算
        # （回调抛错 / NT 事件属性漂移被吞），第二根会再提交 100 ⇒ 当日 2 笔，本腿红。
        rows = [
            ("2024-01-02 01:30:00", 10.0, 1_000),
            ("2024-01-02 01:31:00", 10.0, 1_000),
        ]
        signals = SignalStream([TargetWeight(SYMBOL, 0.5, dt.datetime(2024, 1, 2, 1, 30))])
        run = _run_replay(bar_type_str=MINUTE_BAR, rows=rows, signals=signals)

        submissions = [d for d in run.strategy.decisions if d.submitted > 0]
        self.assertEqual(
            1,
            len(submissions),
            f"成交必须消耗当日参与率预算（否则同一日后继 bar 会重复提交）："
            f"{[(d.side, d.submitted, d.reason) for d in run.strategy.decisions]}",
        )
        self.assertEqual(100.0, submissions[0].submitted, "首片 == cap（floor(0.10×1000)）")
        self.assertTrue(
            any(d.reason == "participation cap reached" for d in run.strategy.decisions),
            f"第二根应判「cap 已达」：{[d.reason for d in run.strategy.decisions]}",
        )


class OrderReceiptAccountingTest(unittest.TestCase):
    """K14-R1 B · 拒单被记账且不重复提交；``_traded_today`` 只计成交。"""

    def test_rejected_order_is_recorded_and_not_retried(self) -> None:
        # The venue cash (1,000 CNY) is far below the intended 0.5 × 1,000,000
        # notional, so NT rejects the buy. The rejected leg must be recorded and
        # then stop — not resubmit the identical order on every bar (the observed
        # pre-fix loop).
        run = _run_replay(
            bar_type_str=DAY_BAR,
            rows=_daily_rows([10.0, 10.0, 10.0, 10.0]),
            signals=SignalStream([TargetWeight(SYMBOL, 0.5, dt.date(2024, 1, 2))]),
            capital=1_000.0,
            capital_base=1_000_000.0,
        )
        submissions = [d for d in run.strategy.decisions if d.side in ("buy", "sell")]
        self.assertEqual(
            1,
            len(submissions),
            f"a rejected order must not be retried every bar: "
            f"{[(d.side, d.reason) for d in run.strategy.decisions]}",
        )
        self.assertGreaterEqual(run.strategy.rejections, 1, "the rejection must be observable")
        self.assertEqual(0.0, run.strategy._traded_today, "a rejected order is not a fill")


class MultiSymbolCapitalBudgetTest(unittest.TestCase):
    """K14-R1 A · 权重是组合总权重，多标的不得资金超配（腿不静默消失）。"""

    def test_two_symbols_both_take_positions(self) -> None:
        from sidecar.engine import run_backtest
        from sidecar.runspec import RunSpec

        symbols = ("600000.SH", "000001.SZ")
        tmp = Path(tempfile.mkdtemp(prefix="nt-k14-multi-"))
        try:
            catalog = ParquetDataCatalog(str(tmp))
            catalog.write_instruments([make_equity(s, VENUE) for s in symbols])
            for s in symbols:
                _write_bars(
                    catalog,
                    BarType.from_str(f"{s}.{VENUE}-1-DAY-LAST-EXTERNAL"),
                    _daily_rows([10.0, 10.0, 10.0, 10.0, 10.0, 10.0]),
                )
            spec = RunSpec.from_json(
                {
                    "strategy": "weight_replay",
                    "stock_pool": list(symbols),
                    "start_date": "2024-01-02",
                    "end_date": "2024-01-07",
                }
            )
            result = run_backtest(spec, catalog_path=str(tmp))
        finally:
            shutil.rmtree(tmp, ignore_errors=True)

        self.assertEqual("completed", result["status"])
        # Both legs must fill. The A-share fee total is a deterministic fill-side
        # observable: two legs pay exactly twice one leg's fees, whereas the broken
        # (full-capital-per-instance) path filled only one leg.
        single_leg_fees = (
            ashare.compute_fees(0.5 * (CAPITAL / 2), is_sell=False).total
            + ashare.compute_fees(0.5 * (CAPITAL / 2), is_sell=True).total
        )
        detail = (
            f"orders={result['total_orders']} positions={result['total_positions']} "
            f"pnl={result['nt_stats']['pnls']}"
        )
        self.assertAlmostEqual(
            -2 * single_leg_fees,
            result["nt_stats"]["pnls"]["CNY"]["PnL (total)"],
            delta=1.0,
            msg=f"both legs must fill, not just one ({detail})",
        )
        self.assertEqual(2, result["total_positions"], f"both legs must take a position ({detail})")


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
