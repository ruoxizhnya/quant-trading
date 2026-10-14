"""D7 测试（1/2）：市场档案表 + fail-closed 解析。

Run from ``nt-sidecar/``::

    python -m unittest tests.test_d7_market_profiles -v

钉住三件事：

1. **``CN`` 档案 = 存量行为**（K13–K16 资产不动）：币种 CNY、费率模型
   ashare、执行模式 weight_replay——改任何一项都意味着国内线被动了。
2. **``US`` 档案 = NT 原生**：USD、NT 自带费率模型、原生执行模式；
   **不是** A 股档案的复制品（复制会让「原生」二字落空）。
3. **未登记市场 fail-closed**：解析拒绝而非回退 ``CN``；装配期（``build_server``）
   就把未登记市场挡在启动之外。
"""

from __future__ import annotations

import unittest

from sidecar.catalog import BASE_CURRENCY, PRICE_INCREMENT, PRICE_PRECISION
from sidecar.errors import MarketError
from sidecar.markets import (
    DEFAULT_MARKET,
    EXECUTION_NATIVE,
    EXECUTION_WEIGHT_REPLAY,
    FEE_MODEL_ASHARE,
    FEE_MODEL_NATIVE,
    KNOWN_EXECUTION_MODES,
    KNOWN_FEE_MODELS,
    MARKET_PROFILES,
    MarketProfile,
    known_markets,
    resolve_market,
    validate_profile,
)
from sidecar.server import build_server


class MarketRegistryTest(unittest.TestCase):
    def test_registry_shape(self) -> None:
        self.assertEqual(known_markets(), ("CN", "US"))
        self.assertEqual(DEFAULT_MARKET, "CN", "缺省市场必须是 A 股线——存量部署行为不变")

    def test_cn_profile_is_the_legacy_behaviour(self) -> None:
        """CN 档案必须逐字等于 K13–K16 时期的硬编码取值。"""
        cn = MARKET_PROFILES["CN"]
        self.assertEqual(cn.currency, BASE_CURRENCY)
        self.assertEqual(cn.price_precision, PRICE_PRECISION)
        self.assertEqual(cn.price_increment, PRICE_INCREMENT)
        self.assertEqual(cn.fee_model, FEE_MODEL_ASHARE)
        self.assertEqual(cn.execution_mode, EXECUTION_WEIGHT_REPLAY)

    def test_us_profile_is_native_not_a_copy(self) -> None:
        """US 档案与 CN 必须**不同**——否则「NT 原生模式」只是块招牌。"""
        us = MARKET_PROFILES["US"]
        self.assertEqual(us.currency, "USD")
        self.assertEqual(us.fee_model, FEE_MODEL_NATIVE)
        self.assertEqual(us.execution_mode, EXECUTION_NATIVE)
        self.assertNotEqual(us.execution_mode, MARKET_PROFILES["CN"].execution_mode)
        self.assertNotEqual(us.fee_model, MARKET_PROFILES["CN"].fee_model)

    def test_every_profile_is_internally_valid(self) -> None:
        for name, profile in MARKET_PROFILES.items():
            with self.subTest(market=name):
                validate_profile(profile)  # 词表封闭，拼错字装配期炸

    def test_validate_profile_rejects_unknown_vocabulary(self) -> None:
        bad_mode = MarketProfile("XX", "USD", 2, "0.01", FEE_MODEL_NATIVE, "not-a-mode")
        with self.assertRaises(MarketError) as ctx:
            validate_profile(bad_mode)
        self.assertIn("execution_mode", str(ctx.exception))
        self.assertIn("not-a-mode", str(ctx.exception))

        bad_fee = MarketProfile("XX", "USD", 2, "0.01", "not-a-fee-model", EXECUTION_NATIVE)
        with self.assertRaises(MarketError) as ctx2:
            validate_profile(bad_fee)
        self.assertIn("fee_model", str(ctx2.exception))

    def test_vocabularies_are_closed(self) -> None:
        self.assertEqual(sorted(KNOWN_EXECUTION_MODES), [EXECUTION_NATIVE, EXECUTION_WEIGHT_REPLAY])
        self.assertEqual(sorted(KNOWN_FEE_MODELS), [FEE_MODEL_ASHARE, FEE_MODEL_NATIVE])


class ResolveMarketTest(unittest.TestCase):
    def test_resolves_known_markets_case_insensitively(self) -> None:
        self.assertEqual(resolve_market("us").market, "US")
        self.assertEqual(resolve_market(" CN ").market, "CN")

    def test_unknown_market_is_refused_not_fallen_back(self) -> None:
        """核心 fail-closed 腿：未登记市场 → 拒绝，且**不**回退 CN。

        回退的代价不是报错而是**一份用错规则算出来的、看起来完全正常的回执**
        ——与 FR-1.3「不得静默回退内核」同一个病。
        """
        with self.assertRaises(MarketError) as ctx:
            resolve_market("HK")
        msg = str(ctx.exception)
        self.assertIn("HK", msg, "错误必须点名被拒的市场")
        self.assertIn("CN", msg, "错误必须给出已登记市场集合")
        self.assertIn("US", msg)
        self.assertIn("no silent fallback", msg, "错误必须说明是拒绝而非回退")

    def test_empty_market_is_refused(self) -> None:
        for value in ("", "   "):
            with self.subTest(value=value):
                with self.assertRaises(MarketError):
                    resolve_market(value)
        with self.assertRaises(MarketError):
            resolve_market(None)  # type: ignore[arg-type]


class AssemblyTimeGateTest(unittest.TestCase):
    """市场在**装配期**解析：未登记市场让进程起不来，而不是运行期跑错规则。"""

    def test_build_server_accepts_registered_market(self) -> None:
        server = build_server(host="127.0.0.1", port=0, catalog_path=".", market="us")
        try:
            self.assertEqual(server.market, "US")
        finally:
            server.server_close()

    def test_build_server_refuses_unknown_market(self) -> None:
        with self.assertRaises(MarketError) as ctx:
            build_server(host="127.0.0.1", port=0, catalog_path=".", market="HK")
        self.assertIn("HK", str(ctx.exception))

    def test_default_market_keeps_legacy_start_command(self) -> None:
        """不带 --market 的启动命令行为不变（缺省 CN）。"""
        server = build_server(host="127.0.0.1", port=0, catalog_path=".")
        try:
            self.assertEqual(server.market, "CN")
        finally:
            server.server_close()


if __name__ == "__main__":
    unittest.main()
