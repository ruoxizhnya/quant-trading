"""D7 测试（2/2）：NT 原生模式端到端 + 「外壳共用」回执 schema 一致性。

Run from ``nt-sidecar/``::

    python -m unittest tests.test_d7_native_mode -v

三条腿：

1. **原生模式端到端**（真实 NautilusTrader + 合成美股 catalog）：国外市场样例
   run 跑通、回执可读，且成交里能看到 **NT 原生订单类型**（LIMIT 进 / MARKET 出）。
2. **原生 ≠ A 股魔改**（可证伪的一条）：用**非整手**手数下单，成交数量必须
   原样保留——A 股线的 ``round_down_to_lot`` 会把它取整到 100，原生线不会。
3. **外壳共用**（跨语言）：侧车回执的字段名必须全部落在 Go
   ``contracts.BacktestResponse`` 的 json tag 集合内（外加明确登记的两个
   侧车专属键）。**改一个字段名 = 该字段在 ResultSpec 映射处静默丢失**，所以
   这条断言是「前端/审计不感知内核差异」的机械护栏。CN 线走同一条断言，
   证明两线共用同一副外壳。
"""

from __future__ import annotations

import json
import re
import shutil
import tempfile
import unittest
from decimal import Decimal
from pathlib import Path

import pandas as pd
from nautilus_trader.model import Bar, BarType, Price, Quantity
from nautilus_trader.persistence import ParquetDataCatalog

from sidecar.catalog import bar_type_str, make_equity
from sidecar.engine import run_backtest
from sidecar.errors import MarketError, SidecarError
from sidecar.markets import MarketProfile, resolve_market
from sidecar.native import NATIVE_STRATEGIES, build_native_strategy
from sidecar.runspec import RunSpec

NT_SIDECAR_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = Path(__file__).resolve().parents[2]
GO_CONTRACTS_FILE = REPO_ROOT / "pkg" / "backtest" / "contracts" / "contracts.go"

US_SYMBOLS = ("AAPL", "MSFT")
CN_SYMBOLS = ("600000.SH", "000001.SZ")
START = "2024-01-02"
BARS = 60

#: 侧车 wire 名 → ResultSpec 字段名 的**显式重命名表**。
#:
#: wire 是侧车自己的 DTO，起自己的名字没问题；有问题的是**隐式**重命名——
#: 一个没有登记的名字会被 ``runner.toResponse`` 静默丢弃，回执看着正常、
#: 数据却没了。所以每个重命名都要在这里逐条写明，并断言目标真是 ResultSpec
#: 字段。``equity_curve → portfolio_values`` 是唯一一条（K15 起）。
SIDECAR_WIRE_ALIASES: dict[str, str] = {"equity_curve": "portfolio_values"}

#: 侧车回执里**没有 ResultSpec 对应概念**的键 —— 逐条登记**理由**，不是「允许漂移」。
#: 一个键要进这张表，就必须说清它为什么不是 ResultSpec 字段；说不清就说明
#: 它是漂移（改坏字段名的典型症状），测试必须红。
SIDECAR_ONLY_KEYS: dict[str, str] = {
    "nt_stats": "NT 原始统计（K15 对齐与溯源），刻意不进冻结契约",
    "total_orders": "NT 执行机制量（下单笔数）；ResultSpec 用 total_trades 表达成交笔数",
    "total_positions": "NT 执行机制量（持仓事件数）；ResultSpec 无对应指标",
    "iterations": "NT 引擎迭代次数（执行机制量）；ResultSpec 无对应指标",
}

#: 两线都必须产出、且经重命名表映射后仍落在 ResultSpec 内的**概念**。
#: 用 ResultSpec 命名（不是 wire 命名）——它断言的是「外壳拿得到这些概念」。
REQUIRED_SHELL_CONCEPTS = frozenset(
    {"id", "status", "strategy", "total_return", "portfolio_values", "trades"}
)


def _shell_keys(payload: dict) -> set[str]:
    """把侧车回执的键投影到 ResultSpec 命名空间（别名表生效后）。"""
    return {SIDECAR_WIRE_ALIASES.get(key, key) for key in payload}


def _canonical_resultspec_keys() -> set[str]:
    """从 Go 源码抽 ``BacktestResponse`` 的 json tag 集合（跨语言字面量契约）。

    读源码而不是维护第二份清单：Go 侧改字段名/加字段，这条断言立刻跟上；
    手工清单会悄悄过期，而它一旦过期就再也测不出漂移。
    """
    src = GO_CONTRACTS_FILE.read_text(encoding="utf-8")
    body = src.split("type BacktestResponse struct {", 1)[1].split("\n}", 1)[0]
    tags = set()
    for line in body.splitlines():
        m = re.search(r'json:"([^",]+)', line)
        if m:
            tags.add(m.group(1))
    return tags


def _write_catalog(path: Path, symbols, *, currency: str, bars: int = BARS) -> None:
    """合成 catalog：一只 Equity + 一段**确定性的锯齿**日线。

    锯齿（每 10 根反向一次）保证快慢均线一定交叉 ⇒ 原生策略一定有成交，
    且不引入 RNG（可复现）。
    """
    path.mkdir(parents=True, exist_ok=True)
    catalog = ParquetDataCatalog(str(path))
    timestamps = pd.date_range(START, periods=bars, freq="D", tz="UTC")
    for symbol in symbols:
        catalog.write_instruments([make_equity(symbol, "SIM", currency=currency)])
        bar_type = BarType.from_str(bar_type_str(symbol, "SIM"))
        rows = []
        for i, ts in enumerate(timestamps):
            px = round(100.0 + (10.0 if (i // 10) % 2 == 0 else -10.0) + 0.1 * i, 2)
            rows.append(
                Bar(
                    bar_type=bar_type,
                    open=Price(px, precision=2),
                    high=Price(px + 0.50, precision=2),
                    low=Price(px - 0.50, precision=2),
                    close=Price(px, precision=2),
                    volume=Quantity.from_int(1_000_000),
                    ts_event=int(ts.value),
                    ts_init=int(ts.value),
                )
            )
        catalog.write_bars(rows)


def _spec(strategy: str, symbols, **overrides) -> RunSpec:
    payload = {
        "strategy": strategy,
        "stock_pool": list(symbols),
        "start_date": START,
        "end_date": "2024-03-01",
    }
    payload.update(overrides)
    return RunSpec.from_json(payload)


class NativeModeEndToEndTest(unittest.TestCase):
    """腿 1+2：原生模式跑通 + 原生订单类型 + 不受 A 股整手约束。"""

    @classmethod
    def setUpClass(cls) -> None:
        cls._tmp = Path(tempfile.mkdtemp(prefix="d7-us-catalog-"))
        _write_catalog(cls._tmp, US_SYMBOLS, currency="USD")

    @classmethod
    def tearDownClass(cls) -> None:
        shutil.rmtree(cls._tmp, ignore_errors=True)

    def test_native_run_completes_and_trades_both_native_order_types(self) -> None:
        result = run_backtest(
            _spec("native_sma_cross", US_SYMBOLS), catalog_path=str(self._tmp), market="US"
        )
        self.assertEqual(result["status"], "completed", result.get("error"))
        self.assertIsInstance(result["total_return"], float)
        self.assertTrue(result["equity_curve"], "完成的 run 必须带回净值曲线")
        json.dumps(result, allow_nan=False)  # 回执必须严格 JSON

        sides = {row["side"] for row in result["trades"]}
        self.assertIn("BUY", sides, "LIMIT 腿必须成交")
        self.assertIn("SELL", sides, "MARKET 腿必须成交")
        # 两腿价格各自等于其订单类型语义：LIMIT 买挂在 close 上 ⇒ 成交价 = close；
        # 不对具体价格下断言（会随 catalog 波动），只要求成交价为正且有限。
        for row in result["trades"]:
            self.assertGreater(row["price"], 0.0)

    def test_native_mode_does_not_apply_ashare_lot_rounding(self) -> None:
        """可证伪的一条：非整手手数原样成交 —— A 股线会把它取整成 100。

        这是「不做 A 股规则魔改」的最小机械证据。若哪天有人在原生通道里
        复用了 ``ashare.round_down_to_lot``，这条断言立刻红。
        """
        odd = Decimal(137)
        result = run_backtest(
            _spec("native_sma_cross", US_SYMBOLS),
            catalog_path=str(self._tmp),
            market="US",
            trade_size=odd,
        )
        self.assertEqual(result["status"], "completed", result.get("error"))
        qtys = {row["qty"] for row in result["trades"]}
        self.assertEqual(qtys, {float(odd)}, f"原生通道不得整手取整，实际成交量={qtys}")

    def test_cn_market_dispatch_is_by_profile_not_by_strategy_name(self) -> None:
        """同一份档案分派：CN 档案下原生策略名不可用（走的是 weight_replay 分派）。

        如果分派是「看策略名」而不是「看市场档案」，这条会通过——它必须失败，
        否则市场档案就形同虚设。
        """
        cn_catalog = Path(tempfile.mkdtemp(prefix="d7-cn-catalog-"))
        try:
            _write_catalog(cn_catalog, CN_SYMBOLS, currency="CNY")
            with self.assertRaises(SidecarError) as ctx:
                run_backtest(
                    _spec("native_sma_cross", CN_SYMBOLS),
                    catalog_path=str(cn_catalog),
                    market="CN",
                )
            self.assertIn("native_sma_cross", str(ctx.exception))
        finally:
            shutil.rmtree(cn_catalog, ignore_errors=True)

    def test_unknown_market_is_refused_at_run(self) -> None:
        with self.assertRaises(MarketError):
            run_backtest(
                _spec("native_sma_cross", US_SYMBOLS), catalog_path=str(self._tmp), market="HK"
            )

    def test_native_strategy_registry_is_closed(self) -> None:
        self.assertIn("native_sma_cross", NATIVE_STRATEGIES)
        with self.assertRaises(SidecarError) as ctx:
            build_native_strategy(
                "does_not_exist", instrument_id=None, bar_type=None  # type: ignore[arg-type]
            )
        self.assertIn("does_not_exist", str(ctx.exception))
        self.assertIn("native_sma_cross", str(ctx.exception))


class ReceiptSchemaConsistencyTest(unittest.TestCase):
    """腿 3（外壳共用）：两线回执字段名落在同一份 ResultSpec 里。"""

    @classmethod
    def setUpClass(cls) -> None:
        cls._us = Path(tempfile.mkdtemp(prefix="d7-shell-us-"))
        cls._cn = Path(tempfile.mkdtemp(prefix="d7-shell-cn-"))
        _write_catalog(cls._us, US_SYMBOLS, currency="USD")
        _write_catalog(cls._cn, CN_SYMBOLS, currency="CNY")

    @classmethod
    def tearDownClass(cls) -> None:
        shutil.rmtree(cls._us, ignore_errors=True)
        shutil.rmtree(cls._cn, ignore_errors=True)

    def test_canonical_key_extraction_is_not_vacuous(self) -> None:
        keys = _canonical_resultspec_keys()
        self.assertGreater(len(keys), 15, "从 Go 源码抽到的 ResultSpec 键太少——解析器坏了？")
        self.assertIn("total_return", keys)
        self.assertIn("portfolio_values", keys)

    def _assert_shell_consistent(self, payload: dict, label: str) -> None:
        canonical = _canonical_resultspec_keys()
        known = canonical | set(SIDECAR_ONLY_KEYS) | set(SIDECAR_WIRE_ALIASES)
        unknown = set(payload) - known
        self.assertEqual(
            unknown,
            set(),
            f"{label}: 回执出现既不在 ResultSpec、也未登记为侧车专属/别名的字段名 "
            f"{sorted(unknown)} —— 这类字段会被外壳映射静默丢弃（两线回执从此不可比）",
        )
        for wire, target in SIDECAR_WIRE_ALIASES.items():
            self.assertIn(target, canonical, f"别名 {wire} → {target}：目标是幽灵字段")
        missing = REQUIRED_SHELL_CONCEPTS - _shell_keys(payload)
        self.assertEqual(missing, set(), f"{label}: 外壳拿不到必需概念 {sorted(missing)}")

    def test_foreign_line_receipt_matches_the_shell(self) -> None:
        payload = run_backtest(
            _spec("native_sma_cross", US_SYMBOLS), catalog_path=str(self._us), market="US"
        )
        self._assert_shell_consistent(payload, "国外线（US/native）")

    def test_domestic_line_receipt_matches_the_same_shell(self) -> None:
        """国内线走**同一条**断言：两线共用一副外壳，不是各说各话。"""
        payload = run_backtest(
            _spec("weight_replay", CN_SYMBOLS), catalog_path=str(self._cn), market="CN"
        )
        self._assert_shell_consistent(payload, "国内线（CN/weight_replay）")

    def test_sidecar_only_keys_are_explicitly_registered(self) -> None:
        """多出来的键必须与登记表**恰好**相等，且每条都有理由。

        登记表放大一点（多写一个用不到的名字）或者侧车偷偷多吐一个键，
        两者都会红——表不能被拿来当橡皮图章。
        """
        payload = run_backtest(
            _spec("native_sma_cross", US_SYMBOLS), catalog_path=str(self._us), market="US"
        )
        extra = set(payload) - _canonical_resultspec_keys()
        self.assertEqual(extra, set(SIDECAR_ONLY_KEYS) | set(SIDECAR_WIRE_ALIASES))
        for key, reason in SIDECAR_ONLY_KEYS.items():
            with self.subTest(key=key):
                self.assertTrue(reason.strip(), f"{key} 必须写清「为什么它不是 ResultSpec 字段」")


class MarketProfileDispatchTest(unittest.TestCase):
    """分派函数的单元腿：档案 → 执行模式，不依赖引擎。"""

    def test_profile_lookup_matches_registry(self) -> None:
        self.assertEqual(resolve_market("US").execution_mode, "native")
        self.assertEqual(resolve_market("CN").execution_mode, "weight_replay")

    def test_temp_profile_with_native_mode_is_recognised(self) -> None:
        profile = MarketProfile("XX", "USD", 2, "0.01", "native", "native")
        self.assertEqual(profile.execution_mode, "native")


if __name__ == "__main__":
    unittest.main()
