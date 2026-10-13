"""K15 mapping-layer tests: RunSpec.signals parsing + equity synthesis + trades.

Run from ``nt-sidecar/``::

    python -m unittest tests.test_k15_map -v

The equity synthesis is the dual-run alignment's floor: it must reproduce the
Go tracker's ``totalValue = cash + positionsValue`` on the shared calendar, or
every downstream diff attributes formula drift to the execution mechanism.
Destruction legs (改坏 → 红 → 还原) are marked inline.
"""

from __future__ import annotations

import datetime as dt
import unittest

import pandas as pd

from sidecar.engine import map_trades, synth_equity_curve
from sidecar.errors import RunSpecError
from sidecar.runspec import RunSpec


def _account_report(events: list[tuple[str, float]]) -> pd.DataFrame:
    """``(ts, total)`` events → a minimal account report shaped like NT's."""
    index = pd.DatetimeIndex([pd.Timestamp(ts, tz="UTC") for ts, _ in events])
    return pd.DataFrame(
        {"total": [t for _, t in events]},
        index=index,
    )


def _fills(rows: list[tuple[str, str, str, float, float, float, str]]) -> pd.DataFrame:
    """``(instrument_id, side, qty, px, fee, ts, trade_id)`` → fills report."""
    return pd.DataFrame(
        {
            "instrument_id": [r[0] for r in rows],
            "order_side": [r[1] for r in rows],
            "last_qty": [r[2] for r in rows],
            "last_px": [r[3] for r in rows],
            "commission": [r[4] for r in rows],
            "ts_event": [pd.Timestamp(r[5], tz="UTC") for r in rows],
            "trade_id": [r[6] for r in rows],
        }
    )


class RunSpecSignalsTest(unittest.TestCase):
    """K15 · wire-level signal rows are fail-closed validated."""

    def _payload(self, signals: object) -> dict:
        return {
            "strategy": "weight_replay",
            "stock_pool": ["000001.SZ"],
            "start_date": "2024-01-02",
            "end_date": "2024-03-29",
            "signals": signals,
        }

    def test_valid_signals_round_trip(self):
        spec = RunSpec.from_json(
            self._payload(
                [
                    {"symbol": " 000001.SZ ", "weight": 0.5, "as_of": "2024-01-02"},
                    {"symbol": "600000.SH", "weight": 0.0, "as_of": "2024-03-28"},
                ]
            )
        )
        self.assertEqual(len(spec.signals), 2)
        self.assertEqual(spec.signals[0]["symbol"], "000001.SZ")
        self.assertEqual(spec.signals[0]["weight"], 0.5)

    def test_absent_signals_means_sample_stream(self):
        spec = RunSpec.from_json({k: v for k, v in self._payload(None).items() if k != "signals"})
        self.assertEqual(spec.signals, ())

    def test_weight_above_one_rejected(self):
        with self.assertRaisesRegex(RunSpecError, r"signals\[0\]\.weight"):
            RunSpec.from_json(self._payload([{"symbol": "S", "weight": 1.01, "as_of": "2024-01-02"}]))

    def test_negative_weight_rejected(self):
        with self.assertRaisesRegex(RunSpecError, r"signals\[0\]\.weight"):
            RunSpec.from_json(self._payload([{"symbol": "S", "weight": -0.1, "as_of": "2024-01-02"}]))

    def test_bad_date_rejected(self):
        with self.assertRaisesRegex(RunSpecError, r"signals\[0\]\.as_of"):
            RunSpec.from_json(self._payload([{"symbol": "S", "weight": 0.5, "as_of": "20240102"}]))

    def test_non_zero_padded_date_rejected(self):
        # L1 · 与 Go time.Parse 同口径："2024-1-2" 在 strptime 下能过，但
        # 两侧校验必须接受**完全相同**的行集合——round-trip 校验拒绝它。
        with self.assertRaisesRegex(RunSpecError, r"zero-padded"):
            RunSpec.from_json(self._payload([{"symbol": "S", "weight": 0.5, "as_of": "2024-1-2"}]))

    def test_blank_symbol_rejected(self):
        with self.assertRaisesRegex(RunSpecError, r"signals\[0\]\.symbol"):
            RunSpec.from_json(self._payload([{"symbol": "  ", "weight": 0.5, "as_of": "2024-01-02"}]))

    def test_non_list_rejected(self):
        with self.assertRaisesRegex(RunSpecError, "'signals' must be a list"):
            RunSpec.from_json(self._payload({"symbol": "S", "weight": 0.5}))

    def test_non_object_row_rejected(self):
        with self.assertRaisesRegex(RunSpecError, r"signals\[0\] must be an object"):
            RunSpec.from_json(self._payload(["buy"]))


class SynthEquityCurveTest(unittest.TestCase):
    """The Go-tracker-mirror formula on a synthetic two-symbol window."""

    CLOSES = {
        "000001.SZ": pd.Series(
            [10.0, 11.0, 12.0, 13.0],
            index=[dt.date(2024, 1, 2 + i) for i in range(4)],
        ),
        "600000.SH": pd.Series(
            [8.0, 8.0, 9.0, 9.0],
            index=[dt.date(2024, 1, 2 + i) for i in range(4)],
        ),
    }

    def test_buy_hold_sell_formula(self):
        # Day 1: buy 100 A @10 → cash -1000-1 (fee). Day 4: sell 100 A @13.
        acct = _account_report(
            [
                ("2024-01-02T00:00:00Z", 1_000_000.0),
                ("2024-01-02T00:00:01Z", 998_999.0),
                ("2024-01-04T00:00:01Z", 1_012_998.0),
            ]
        )
        fills = _fills(
            [
                ("000001.SZ.SIM", "BUY", 100.0, 10.0, "1.00 CNY", "2024-01-02T00:00:00Z", "t1"),
                ("000001.SZ.SIM", "SELL", 100.0, 13.0, "0.00 CNY", "2024-01-04T00:00:00Z", "t2"),
            ]
        )
        curve = synth_equity_curve(
            1_000_000.0,
            acct,
            fills,
            self.CLOSES,
            venue="SIM",
            start_date="2024-01-02",
            end_date="2024-01-05",
        )
        self.assertEqual(
            [p["date"] for p in curve],
            ["2024-01-02", "2024-01-03", "2024-01-04", "2024-01-05"],
        )
        # Day 1 close: cash 998999 + 100×10 = 999999.
        self.assertAlmostEqual(curve[0]["equity"], 999_999.0, places=2)
        self.assertAlmostEqual(curve[0]["positions"], 1_000.0, places=2)
        # Day 2 close: cash unchanged (no events) + 100×11 = 998999 + 1100.
        self.assertAlmostEqual(curve[1]["equity"], 1_000_099.0, places=2)
        # Day 3 (sell day): cash 1012998 + 0 positions (closed same day at close 12? sold @13 → flat).
        self.assertAlmostEqual(curve[2]["equity"], 1_012_998.0, places=2)
        self.assertAlmostEqual(curve[2]["positions"], 0.0, places=2)

    def test_suspension_carries_close_forward(self):
        # B has no bar on day 3: its position value must freeze at day-2 close.
        closes = {
            "600000.SH": pd.Series(
                [8.0, 8.0, 9.0], index=[dt.date(2024, 1, 2), dt.date(2024, 1, 3), dt.date(2024, 1, 4)]
            ),
        }
        acct = _account_report(
            [("2024-01-02T00:00:01Z", 999_200.0)]
        )
        fills = _fills(
            [("600000.SH.SIM", "BUY", 100.0, 8.0, "0.00 CNY", "2024-01-02T00:00:00Z", "t1")]
        )
        curve = synth_equity_curve(
            1_000_000.0, acct, fills, closes, venue="SIM", start_date="2024-01-02", end_date="2024-01-04"
        )
        self.assertAlmostEqual(curve[1]["equity"], 999_200.0 + 800.0, places=2)
        # Day 3 carries the day-2 close (8.0) — suspension, not a 0 mark.
        self.assertAlmostEqual(curve[1]["positions"], 800.0, places=2)

    def test_window_clipped_to_run_dates(self):
        acct = _account_report([("2024-01-02T00:00:00Z", 1_000_000.0)])
        curve = synth_equity_curve(
            1_000_000.0, acct, None, self.CLOSES,
            venue="SIM", start_date="2024-01-02", end_date="2024-01-03",
        )
        self.assertEqual([p["date"] for p in curve], ["2024-01-02", "2024-01-03"])

    def test_missing_account_report_is_empty_not_flat(self):
        self.assertEqual(
            synth_equity_curve(1_000_000.0, None, None, self.CLOSES, venue="SIM"), []
        )
        self.assertEqual(
            synth_equity_curve(1_000_000.0, _account_report([]), None, self.CLOSES, venue="SIM"), []
        )

    def test_no_fills_is_flat_cash_curve(self):
        acct = _account_report([("2024-01-02T00:00:00Z", 1_000_000.0)])
        curve = synth_equity_curve(
            1_000_000.0, acct, None, self.CLOSES,
            venue="SIM", start_date="2024-01-02", end_date="2024-01-03",
        )
        for point in curve:
            self.assertAlmostEqual(point["equity"], 1_000_000.0, places=2)
            self.assertAlmostEqual(point["positions"], 0.0, places=2)


class MapTradesTest(unittest.TestCase):
    def test_rows_mapped_with_venue_suffix_stripped(self):
        fills = _fills(
            [
                ("000001.SZ.SIM", "BUY", 100.0, 7.57, "5.00 CNY", "2024-01-02T00:00:00Z", "t1"),
                ("600000.SH.SIM", "SELL", 200.0, 8.10, "12.34 CNY", "2024-01-03T00:00:00Z", "t2"),
            ]
        )
        trades = map_trades(fills, venue="SIM")
        self.assertEqual([t["symbol"] for t in trades], ["000001.SZ", "600000.SH"])
        self.assertEqual([t["side"] for t in trades], ["BUY", "SELL"])
        self.assertAlmostEqual(trades[0]["commission"], 5.0, places=6)
        self.assertAlmostEqual(trades[1]["commission"], 12.34, places=6)
        # NT reports the A-share fee as one total; components stay zero so the
        # Go diff compares commission+transfer+stamp against this number.
        self.assertEqual(trades[0]["transfer_fee"], 0.0)
        self.assertEqual(trades[0]["stamp_tax"], 0.0)

    def test_empty_report_is_empty(self):
        self.assertEqual(map_trades(None, venue="SIM"), [])
        self.assertEqual(map_trades(pd.DataFrame(), venue="SIM"), [])


if __name__ == "__main__":
    unittest.main()
