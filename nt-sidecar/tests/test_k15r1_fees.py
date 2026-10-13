"""K15-R1 · 最小复现：NT fills 报告的 commission 必须等于 AShareFeeModel 的
``compute_fees(...).total``（全三项），而不是纯佣金。

背景（K15 实验，报告归因②）：真实双跑里 Go 侧每笔费用 = ComputeFees 全三项
精确对上，NT 同一笔 fills 报告的 commission ≈ 纯佣金——过户费分量未体现。
本测试用**单笔已知成交**把现象钉住，然后逐层定位。

Run from ``nt-sidecar/``::

    python -m unittest tests.test_k15r1_fees -v
"""

from __future__ import annotations

import os
import sys
import unittest
from decimal import Decimal

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from sidecar.ashare import compute_fees  # noqa: E402


class FeeModelUnitTest(unittest.TestCase):
    """第一层：FeeModel 本身返回的是全三项 total（与 parity fixture 同源）。"""

    def test_compute_fees_includes_transfer_fee(self):
        # v = 7.57 × 33000 = 249,810 → commission 74.943 + transfer 2.498。
        fees = compute_fees(249_810.0, False)
        self.assertAlmostEqual(fees.commission, 74.943, places=2)
        self.assertAlmostEqual(fees.transfer_fee, 2.498, places=2)
        self.assertAlmostEqual(fees.total, 77.441, places=2)

    def test_fee_model_returns_total(self):
        """make_fee_model 的 get_commission 返回 total——用最小 stub 验证。"""
        model = _build_fee_model()
        fee = model.get_commission(
            _StubOrder(side="BUY"), _StubQty(33_000), _StubPrice(7.57), _StubInstrument()
        )
        self.assertAlmostEqual(float(fee.as_double()), 77.441, places=2)


class FillReportFeeRegressionTest(unittest.TestCase):
    """第二层回归腿（K15-R1 收口）：端到端 fills 报告的 commission 必须
    逐笔等于 ``compute_fees(...).total``（全三项）。

    背景：K15 实验（fcd5da3）的 bh/quarterly 报告里 NT fills commission ≈
    纯佣金（74.91/38.94，缺过户费）；复查时同场景（直调与 HTTP 双路径）
    实测均为全三项（77.44/40.24），且账户 cash 与当时报告一字不差——
    即当时实际扣费就是全项、只有 fills 报告列异常（不可重现，疑似当时
    sidecar 进程加载了旧字节码状态）。本腿把「报告列 == 全项」钉成回归
    护栏，防止再次漂移。
    """

    def test_fills_report_commission_is_full_fee_schedule(self):
        from sidecar.engine import run_backtest
        from sidecar.runspec import RunSpec

        spec = RunSpec(
            strategy="weight_replay",
            stock_pool=("000001.SZ", "600000.SH"),
            start_date="2024-01-02",
            end_date="2024-01-02",
            initial_capital=1_000_000.0,
            signals=(
                {"symbol": "000001.SZ", "weight": 0.5, "as_of": "2024-01-02"},
                {"symbol": "600000.SH", "weight": 0.5, "as_of": "2024-01-02"},
            ),
        )
        catalog = os.path.join(
            os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "catalog"
        )
        payload = run_backtest(spec, catalog_path=catalog)
        self.assertEqual(payload["status"], "completed", payload["error"])
        self.assertGreaterEqual(len(payload["trades"]), 2)
        for t in payload["trades"]:
            value = t["qty"] * t["price"]
            expected = compute_fees(value, t["side"] == "SELL").total
            self.assertAlmostEqual(
                t["commission"],
                expected,
                delta=0.011,  # Money 2 位舍入容差
                msg=f"{t['symbol']} {t['side']} qty={t['qty']} px={t['price']}: "
                f"fills commission {t['commission']} != full schedule {expected}",
            )


def _build_fee_model():
    from sidecar.ashare import make_fee_model

    return make_fee_model()


class _StubSide:
    def __init__(self, name: str) -> None:
        self.name = name

    def __eq__(self, other: object) -> bool:
        return isinstance(other, _StubSide) and self.name == other.name


class _StubOrder:
    def __init__(self, side: str) -> None:
        self.side = _StubSide(side)


class _StubQty:
    def __init__(self, value: float) -> None:
        self._value = Decimal(str(value))

    def as_double(self) -> float:
        return float(self._value)


class _StubPrice:
    def __init__(self, value: float) -> None:
        self._value = Decimal(str(value))

    def as_double(self) -> float:
        return float(self._value)


class _StubInstrument:
    """NT ``Money`` 构造要求真 ``Currency``——用 from_str 的 CNY 单例。"""

    def __init__(self) -> None:
        from nautilus_trader.model import Currency

        self.quote_currency = Currency.from_str("CNY")


if __name__ == "__main__":
    unittest.main()
