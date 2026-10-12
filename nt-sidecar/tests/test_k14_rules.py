"""K14 · A-share minimal rule set — unit + parity tests.

These are the decisive reverse legs the ticket asks for, exercised at the pure
rule layer (fast, no engine needed):

* T+1       — a sell is truncated to the start-of-day sellable quantity
* price limit — a buy is refused at the upper limit, a sell at the lower limit
* fees      — parity with the Go ``portfolio.ComputeFees`` at **exact** tolerance
              (the fixture is generated from the Go implementation)
* D_max     — the participation cap is a hard floor() limit

Run with::

    python -m unittest discover -s tests -t .      (from nt-sidecar/)
"""

from __future__ import annotations

import datetime as dt
import json
import unittest
from pathlib import Path

from sidecar import ashare
from sidecar.signals import SignalStream, TargetWeight, sample_signals

FIXTURE = Path(__file__).resolve().parent / "fixtures" / "ashare_fees_parity.json"


class LotRoundingTest(unittest.TestCase):
    def test_rounds_down_to_whole_lots(self) -> None:
        self.assertEqual(0.0, ashare.round_down_to_lot(0))
        self.assertEqual(0.0, ashare.round_down_to_lot(99))
        self.assertEqual(100.0, ashare.round_down_to_lot(100))
        self.assertEqual(100.0, ashare.round_down_to_lot(199))
        self.assertEqual(200.0, ashare.round_down_to_lot(250))

    def test_never_negative(self) -> None:
        self.assertEqual(0.0, ashare.round_down_to_lot(-500))


class PriceLimitTest(unittest.TestCase):
    def test_main_board_limits_round_to_cent(self) -> None:
        self.assertEqual((11.0, 9.0), ashare.limit_prices(10.0))
        # 3.33*1.1 = 3.663 -> 3.66 ; 3.33*0.9 = 2.997 -> 3.00 (half away from zero)
        self.assertEqual((3.66, 3.0), ashare.limit_prices(3.33))

    def test_round_to_cent_matches_go_math_round(self) -> None:
        self.assertEqual(1.01, ashare.round_to_cent(1.006))
        self.assertEqual(1.99, ashare.round_to_cent(1.994))
        self.assertEqual(2.0, ashare.round_to_cent(2.0))
        self.assertEqual(0.0, ashare.round_to_cent(0.0))


class T1SellableTest(unittest.TestCase):
    def test_only_yesterday_close_is_sellable(self) -> None:
        self.assertEqual(500.0, ashare.sellable_quantity(500))
        self.assertEqual(0.0, ashare.sellable_quantity(-10))
        self.assertEqual(0.0, ashare.sellable_quantity(0))


class ParticipationCapTest(unittest.TestCase):
    def test_cap_is_floor_of_dmax_times_volume(self) -> None:
        self.assertEqual(100_000, ashare.participation_cap(1_000_000, 0.10))
        self.assertEqual(500, ashare.participation_cap(1_000, 0.50))
        self.assertEqual(0, ashare.participation_cap(0, 0.10))

    def test_non_positive_dmax_is_a_caller_bug(self) -> None:
        # fail-closed: a zero cap must not silently forbid (or silently ignore) trading.
        with self.assertRaises(ValueError):
            ashare.participation_cap(1_000, 0.0)
        with self.assertRaises(ValueError):
            ashare.participation_cap(1_000, -0.1)


class FeeParityTest(unittest.TestCase):
    """K14 ⑤ 验收 · 费率与 Go pkg/portfolio.ComputeFees 同输入对拍（容差 0）。"""

    @classmethod
    def setUpClass(cls) -> None:
        cls.rows = json.loads(FIXTURE.read_text())

    def test_matches_go_exactly(self) -> None:
        self.assertGreater(len(self.rows), 0, "fixture must not be empty")
        for row in self.rows:
            with self.subTest(trade_value=row["trade_value"], is_sell=row["is_sell"]):
                fees = ashare.compute_fees(row["trade_value"], row["is_sell"])
                self.assertEqual(row["commission"], fees.commission)
                self.assertEqual(row["transfer_fee"], fees.transfer_fee)
                self.assertEqual(row["stamp_tax"], fees.stamp_tax)
                self.assertEqual(row["total"], fees.total)

    def test_stamp_tax_is_sell_side_only(self) -> None:
        buy = ashare.compute_fees(100_000.0, is_sell=False)
        sell = ashare.compute_fees(100_000.0, is_sell=True)
        self.assertEqual(0.0, buy.stamp_tax)
        self.assertGreater(sell.stamp_tax, 0.0)
        self.assertGreater(sell.total, buy.total)

    def test_minimum_commission_floor(self) -> None:
        # 16,000 * 0.0003 = 4.8 < 5 -> floored to 5.
        self.assertEqual(5.0, ashare.compute_fees(16_000.0, is_sell=False).commission)


class NoLookAheadTest(unittest.TestCase):
    """防前视 · as-of 消费语义：未来信号物理不可见。"""

    def setUp(self) -> None:
        self.stream = SignalStream.from_rows(
            [
                {"symbol": "600000.SH", "weight": 0.3, "as_of": "2024-01-10"},
                {"symbol": "600000.SH", "weight": 0.6, "as_of": "2024-01-20"},
            ]
        )

    def test_future_signal_is_invisible(self) -> None:
        day = dt.date(2024, 1, 15)
        self.assertEqual(0.3, self.stream.target_weight("600000.SH", day))
        self.assertEqual(0.3, self.stream.as_of(day)["600000.SH"].weight)

    def test_before_first_signal_is_empty(self) -> None:
        self.assertEqual({}, self.stream.as_of(dt.date(2024, 1, 5)))
        self.assertIsNone(self.stream.target_weight("600000.SH", dt.date(2024, 1, 5)))

    def test_latest_as_of_wins(self) -> None:
        self.assertEqual(0.6, self.stream.target_weight("600000.SH", dt.date(2024, 1, 25)))

    def test_sample_stream_exits_flat(self) -> None:
        stream = sample_signals(
            "600000.SH",
            enter=dt.date(2024, 1, 5),
            exit_on=dt.date(2024, 1, 15),
        )
        self.assertEqual(0.5, stream.target_weight("600000.SH", dt.date(2024, 1, 10)))
        self.assertEqual(0.0, stream.target_weight("600000.SH", dt.date(2024, 1, 20)))


class TargetWeightValidationTest(unittest.TestCase):
    def test_weight_out_of_range_rejected(self) -> None:
        with self.assertRaises(ValueError):
            TargetWeight(symbol="600000.SH", weight=1.5, as_of=dt.date(2024, 1, 1))
        with self.assertRaises(ValueError):
            TargetWeight(symbol="600000.SH", weight=-0.1, as_of=dt.date(2024, 1, 1))


if __name__ == "__main__":
    unittest.main()
