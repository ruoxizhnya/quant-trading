"""D1 翻译层测试：sidecar/mapping.py 声明式映射 + 跨语言字面量契约。

Run from ``nt-sidecar/``::

    python -m unittest tests.test_d1_mapping -v

钉住四类契约（D1 ③3 / ⑤）：

1. **跨语言字面量**：``FREQ_*`` 与 Go ``pkg/domain/market/frequency.go``
   的 ``Freq*`` 常量逐字相同——测试直接读 Go 源码正则抽取，改一侧即红。
2. **映射表完整性**：``BAR_FIELD_MAP`` 的 canonical 字段名必须存在于
   SPEC.md「规范层 Schema」节 drift:ohlcv 表的 json tag 列（不许引用
   幽灵字段）；NT ``Bar()`` 可消费的规范层字段必须全覆盖。
3. **值变换口径**：price / quantity / UTC 午夜纳秒的构造规则 +
   缺字段 fail-loud（KeyError）+ 非整数成交量 fail-loud（ValueError）。
4. **单一真相与消费**：catalog 的后缀/精度常量来自 mapping；
   export_bars 消费 mapping（模块级钉住），不保留 per-field 硬编码。
"""

from __future__ import annotations

import calendar
import datetime as dt
import importlib.util
import re
import unittest
from decimal import Decimal
from pathlib import Path

from sidecar.catalog import DAILY_BAR_SUFFIX, PRICE_PRECISION, bar_type_str
from sidecar.mapping import (
    BAR_FIELD_MAP,
    BAR_SUFFIX_BY_FREQ,
    FREQ_1D,
    FREQ_1M,
    FREQ_15M,
    KNOWN_FREQUENCIES,
    MAPPED_CANONICAL_FIELDS,
    as_price,
    as_quantity,
    as_ts_utc_midnight_ns,
    bar_kwargs_for_row,
    bar_type_for,
)

NT_SIDECAR_DIR = Path(__file__).resolve().parents[1]
REPO_ROOT = Path(__file__).resolve().parents[2]
GO_FREQUENCY_FILE = REPO_ROOT / "pkg" / "domain" / "market" / "frequency.go"
SPEC_FILE = REPO_ROOT / "docs" / "SPEC.md"
EXPORT_BARS_FILE = NT_SIDECAR_DIR / "tools" / "export_bars.py"

#: NT Bar() 可消费的规范层字段（json tag 名）——「加数据类=映射加一行」
#: 后，这个集合必须同步扩大；测试钉住映射表与它逐字一致。
NT_BAR_CONSUMABLE_FIELDS = frozenset({"open", "high", "low", "close", "volume", "date"})


def _sample_row(**overrides: object) -> dict[str, object]:
    """一行规范层 OHLCV（列名 = SPEC drift:ohlcv 的 json tag）。"""
    row: dict[str, object] = {
        "symbol": "600000.SH",
        "date": dt.date(2024, 1, 2),
        "open": 10.00,
        "high": 10.45,
        "low": 9.98,
        "close": 10.44,
        "volume": 14500,
    }
    row.update(overrides)
    return row


class CrossLanguageLiteralTest(unittest.TestCase):
    """FREQ_* 常量与 Go frequency.go 逐字相同（读源码，双向）。"""

    def test_freq_literals_match_go_source(self) -> None:
        go_src = GO_FREQUENCY_FILE.read_text(encoding="utf-8")
        go_consts = dict(re.findall(r'Freq(1m|15m|1d)\s+Frequency\s*=\s*"([^"]+)"', go_src))
        self.assertEqual(
            go_consts,
            {"1m": FREQ_1M, "15m": FREQ_15M, "1d": FREQ_1D},
            "Python FREQ_* 与 Go Freq* 常量必须逐字相同（跨语言字面量契约）",
        )

    def test_known_frequencies_match_go_wordlist(self) -> None:
        go_src = GO_FREQUENCY_FILE.read_text(encoding="utf-8")
        m = re.search(r"var knownFrequencies = \[\]Frequency\{([^}]*)\}", go_src)
        self.assertIsNotNone(m, "Go knownFrequencies 词表行没找到——frequency.go 结构变了？")
        names = [n.strip() for n in m.group(1).split(",") if n.strip()]
        go_by_name = dict(re.findall(r'(Freq\w+)\s+Frequency\s*=\s*"([^"]+)"', go_src))
        missing = [n for n in names if n not in go_by_name]
        self.assertEqual(missing, [], "knownFrequencies 里的常量名在 frequency.go 找不到定义：%s" % missing)
        resolved = [go_by_name[n] for n in names]
        self.assertEqual(
            sorted(resolved),
            sorted(KNOWN_FREQUENCIES),
            "Python KNOWN_FREQUENCIES 与 Go knownFrequencies 必须同集合",
        )


class FieldMapIntegrityTest(unittest.TestCase):
    """映射表完整性：canonical 字段必须存在于 SPEC，NT 可消费字段全覆盖。"""

    def test_canonical_fields_exist_in_spec(self) -> None:
        spec_src = SPEC_FILE.read_text(encoding="utf-8")
        start = spec_src.index("<!-- drift:ohlcv -->")
        end = spec_src.index("<!-- /drift:ohlcv -->")
        json_tags = set()
        for line in spec_src[start:end].splitlines():
            line = line.strip()
            if not line.startswith("|"):
                continue
            cells = [c.strip() for c in line.strip("|").split("|")]
            if len(cells) >= 3 and cells[0] not in ("字段", "") and not cells[0].startswith("---"):
                json_tags.add(cells[2].split(",")[0])
        self.assertTrue(json_tags, "SPEC drift:ohlcv 表没解析到 json tag 列")
        phantom = set(MAPPED_CANONICAL_FIELDS) - json_tags
        self.assertEqual(
            phantom,
            set(),
            "映射表引用了 SPEC 规范层表里不存在的字段（幽灵字段）：%s" % sorted(phantom),
        )

    def test_nt_bar_consumable_fields_fully_mapped(self) -> None:
        self.assertEqual(
            set(MAPPED_CANONICAL_FIELDS),
            set(NT_BAR_CONSUMABLE_FIELDS),
            "NT Bar() 可消费的规范层字段必须全覆盖（加数据类=映射加一行后同步此集合）",
        )

    def test_field_map_kwargs_shape(self) -> None:
        kwargs = bar_kwargs_for_row(_sample_row())
        self.assertEqual(
            set(kwargs),
            {"open", "high", "low", "close", "volume", "ts_event", "ts_init"},
            "Bar() kwargs 形状漂移——date 喂 ts_event/ts_init 两个参数是预期行为",
        )

    def test_bar_suffix_table_covers_known_freqs_or_fails_loud(self) -> None:
        self.assertIn(FREQ_1D, BAR_SUFFIX_BY_FREQ)
        with self.assertRaises(ValueError):
            bar_type_for("600000.SH", "SIM", FREQ_15M)  # 未实现 → fail-loud，不静默回退


class ValueTransformTest(unittest.TestCase):
    """值变换口径：price / quantity / ts。"""

    def test_price_transform(self) -> None:
        self.assertAlmostEqual(as_price(10.02).as_double(), 10.02)
        self.assertAlmostEqual(as_price("10.45").as_double(), 10.45)
        self.assertEqual(as_price(10).precision, PRICE_PRECISION)
        with self.assertRaises(ValueError):
            as_price("not-a-number")
        with self.assertRaises(TypeError):
            as_price(True)  # bool 是数据损坏信号，不是价格

    def test_quantity_transform(self) -> None:
        self.assertEqual(as_quantity(14500).as_double(), 14500.0)
        self.assertEqual(as_quantity(14500.0).as_double(), 14500.0)  # 整数值 float 放行
        self.assertEqual(as_quantity("3000").as_double(), 3000.0)
        self.assertEqual(as_quantity(Decimal("4300")).as_double(), 4300.0)
        with self.assertRaises(ValueError):
            as_quantity(3000.5)  # 非整数 fail-loud：不悄悄取整
        with self.assertRaises(ValueError):
            as_quantity(Decimal("3000.5"))
        with self.assertRaises(ValueError):
            as_quantity("3000.5")
        with self.assertRaises(TypeError):
            as_quantity(True)
        with self.assertRaises(TypeError):
            as_quantity(None)

    def test_ts_transform_utc_midnight(self) -> None:
        expected = calendar.timegm((2024, 1, 2, 0, 0, 0, 0, 0, 0)) * 10**9
        self.assertEqual(as_ts_utc_midnight_ns(dt.date(2024, 1, 2)), expected)
        self.assertEqual(as_ts_utc_midnight_ns(dt.datetime(2024, 1, 2, 15, 30)), expected)
        self.assertEqual(as_ts_utc_midnight_ns("2024-01-02"), expected)
        self.assertEqual(
            as_ts_utc_midnight_ns(dt.datetime(2024, 1, 2, 8, 0, tzinfo=dt.timezone(dt.timedelta(hours=8)))),
            expected,
            "aware 时间先转 UTC 再取日历日（08:00+08:00 = 00:00Z）",
        )
        with self.assertRaises(ValueError):
            as_ts_utc_midnight_ns("not-a-date")
        with self.assertRaises(TypeError):
            as_ts_utc_midnight_ns(1704067200)

    def test_row_translation_end_to_end(self) -> None:
        kwargs = bar_kwargs_for_row(_sample_row())
        self.assertAlmostEqual(kwargs["open"].as_double(), 10.00)
        self.assertAlmostEqual(kwargs["high"].as_double(), 10.45)
        self.assertAlmostEqual(kwargs["low"].as_double(), 9.98)
        self.assertAlmostEqual(kwargs["close"].as_double(), 10.44)
        self.assertEqual(kwargs["volume"].as_double(), 14500.0)
        expected_ns = calendar.timegm((2024, 1, 2, 0, 0, 0, 0, 0, 0)) * 10**9
        self.assertEqual(kwargs["ts_event"], expected_ns)
        self.assertEqual(kwargs["ts_init"], expected_ns)

    def test_missing_canonical_field_fails_loud(self) -> None:
        row = _sample_row()
        del row["close"]
        with self.assertRaises(KeyError) as ctx:
            bar_kwargs_for_row(row)
        self.assertIn("close", str(ctx.exception))

    def test_extra_columns_ignored(self) -> None:
        row = _sample_row(turnover=146600.0, frequency="1d", visible_at="2024-01-02T07:00:00Z")
        kwargs = bar_kwargs_for_row(row)  # 规范层里 NT Bar 无对应参数的列：原样忽略
        self.assertEqual(set(kwargs), {"open", "high", "low", "close", "volume", "ts_event", "ts_init"})


class SingleSourceAndConsumerTest(unittest.TestCase):
    """单一真相（catalog ← mapping）+ export_bars 消费映射。"""

    def test_catalog_suffix_is_mapping_sourced(self) -> None:
        self.assertEqual(DAILY_BAR_SUFFIX, BAR_SUFFIX_BY_FREQ[FREQ_1D])
        self.assertEqual(bar_type_for("600000.SH", "SIM", FREQ_1D), bar_type_str("600000.SH", "SIM"))

    def test_export_bars_consumes_mapping(self) -> None:
        spec = importlib.util.spec_from_file_location("export_bars_under_test", EXPORT_BARS_FILE)
        self.assertIsNotNone(spec)
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)  # type: ignore[union-attr]
        from sidecar import mapping

        self.assertIs(mod.bar_kwargs_for_row, mapping.bar_kwargs_for_row)
        self.assertIs(mod.bar_type_for, mapping.bar_type_for)
        self.assertEqual(mod.FREQ_1D, mapping.FREQ_1D)


if __name__ == "__main__":
    unittest.main()
