"""按内核翻译层（ADR-031 D1 ③3）：规范层字段 ↔ NT 内核构造参数的声明式映射。

这是「connector 之外无人感知底层表」在 NT 侧落地的那一层：Go 侧直吃
规范层（``pkg/marketdata.Router``），NT 侧经本模块把规范层行数据翻译成
``nautilus_trader`` 的 ``Bar()`` 构造参数。第一个消费者是离线导出工具
``tools/export_bars.py``（K13 数据流）；``tests/test_d1_mapping.py`` 钉住
表的完整性与口径。

**加一种数据类 = 映射加一行。** :data:`BAR_FIELD_MAP` 的每一行把一个
规范层字段（SPEC.md「规范层 Schema」节 drift:ohlcv 表的 json tag 名）
翻译成 NT ``Bar()`` 的一个构造参数加一个值变换；:data:`BAR_SUFFIX_BY_FREQ`
把规范层频率翻译成 NT bar 类型后缀。新增字段/频率只改这两张表，不改
消费者代码——消费者（export_bars）因此没有 per-field 硬编码。

**跨语言字面量契约**：``FREQ_*`` 常量与 Go
``pkg/domain/market/frequency.go`` 的 ``Freq*`` 常量**逐字相同**，
测试读 Go 源码双向钉住（改一侧即红）。

值变换产出 **NT 模型对象**（``Price`` / ``Quantity``）而非裸数值——
``Bar()`` 的类型契约如此（与 K13 既有调用形态逐字一致，不发明新 API）。
时间戳变换产出 UTC 午夜纳秒（A 股日线无日内时刻，与 Go 侧 1d 口径的
UTC 零点同构；K13 既有约定）。
"""

from __future__ import annotations

import datetime as dt
from dataclasses import dataclass
from decimal import Decimal
from typing import Callable, Mapping

from nautilus_trader.model import Price, Quantity

# ── 跨语言字面量契约（Go: pkg/domain/market/frequency.go）──────────
#: 1 分钟 bar。与 Go ``market.Freq1m`` 逐字相同。
FREQ_1M = "1m"
#: 15 分钟 bar（规范层由 1m 聚合，不物化）。与 Go ``market.Freq15m`` 逐字相同。
FREQ_15M = "15m"
#: 日线 bar（qfq 权威源）。与 Go ``market.Freq1d`` 逐字相同。
FREQ_1D = "1d"

#: 规范层频率词表，与 Go ``market.KnownFrequencies()`` 同集合同序。
KNOWN_FREQUENCIES: tuple[str, ...] = (FREQ_1M, FREQ_15M, FREQ_1D)

# ── NT 侧构造精度（Price/Quantity 构造的唯一真相）─────────────────
#: A 股现金股票：CNY，tick 0.01，精度 2。
#: 放在翻译层而非 catalog：Price 构造发生在翻译层（as_price），catalog 的
#: make_equity 反向 import 本模块的常量，保持单一真相。
PRICE_PRECISION = 2
PRICE_INCREMENT = "0.01"

# ── 频率 → NT bar 类型后缀 ────────────────────────────────────────
#: 「加一种数据类=映射加一行」的同款机制：规范层频率进 NT catalog 的后缀表。
#: 1m/15m 的 NT 导出路径尚未实现（规范层路由已支持；export 属后续工单），
#: 到了就加一行——未知频率 fail-loud，不静默落到某个默认后缀。
BAR_SUFFIX_BY_FREQ: dict[str, str] = {
    FREQ_1D: "1-DAY-LAST-EXTERNAL",  # 日线由导出工具外部聚合，故 EXTERNAL
}


def as_price(value: object) -> Price:
    """规范层价格字段 → NT ``Price``（精度 PRICE_PRECISION）。"""
    if isinstance(value, bool):
        raise TypeError(f"mapping: price 收到 bool {value!r}——数据损坏，拒绝构造")
    try:
        return Price(float(value), precision=PRICE_PRECISION)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"mapping: 无法把 {value!r} 构造成 Price：{exc}") from exc


def as_quantity(value: object) -> Quantity:
    """规范层成交量字段 → NT ``Quantity.from_int``。

    **单位是「股」——这是规范层的既定口径**（SPEC.md「规范层 Schema」节声明
    OHLCV.Volume 单位是股；tushare 的「手」在 pkg/data 同步时按 sharesPerLot
    换算，存量由 migration 052 对齐）。本层不再做任何单位换算，只执行一条
    纪律：进来的必须是**整数**。非整数值（3.5、「3000.5」）fail-loud ——
    取整会悄悄改口径，换算属于上游的事，翻译层不悄悄取整。
    """
    if isinstance(value, bool):  # bool 是 int 子类，先挡掉
        raise TypeError(f"mapping: volume 收到 bool {value!r}——数据损坏，拒绝构造")
    if isinstance(value, int):
        qty = value
    elif isinstance(value, float):
        if not value.is_integer():
            raise ValueError(f"mapping: volume {value!r} 不是整数——换算请在规范层做，翻译层不悄悄取整")
        qty = int(value)
    elif isinstance(value, Decimal):
        if value != value.to_integral_value():
            raise ValueError(f"mapping: volume {value!r} 不是整数——换算请在规范层做，翻译层不悄悄取整")
        qty = int(value)
    elif isinstance(value, str):
        qty = int(value.strip())  # 非整数字符串抛 ValueError = fail-loud
    else:
        raise TypeError(f"mapping: 不支持的 volume 类型 {type(value).__name__}（值 {value!r}）")
    return Quantity.from_int(qty)


_EPOCH = dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)


def as_ts_utc_midnight_ns(value: object) -> int:
    """规范层 ``date`` 字段 → UTC 午夜纳秒（``ts_event`` / ``ts_init``）。

    接受 ``dt.date`` / ``dt.datetime`` / ISO 字符串。naive 时间按 UTC
    解释（K13 既有口径：``pd.Timestamp(...).tz_localize("UTC")``），aware
    时间先转换再取日历日。与 Go 侧 1d 口径的 UTC 零点同构——两种内核的
    日线 Date 因此可比（K15 双跑对齐的前提）。
    """
    if isinstance(value, dt.datetime):
        moment = value
    elif isinstance(value, dt.date):
        moment = dt.datetime(value.year, value.month, value.day)
    elif isinstance(value, str):
        try:
            moment = dt.datetime.fromisoformat(value.strip())
        except ValueError as exc:
            raise ValueError(f"mapping: 无法解析日期字符串 {value!r}（期望 ISO 格式）：{exc}") from exc
    else:
        raise TypeError(f"mapping: 不支持的日期类型 {type(value).__name__}（值 {value!r}）")

    if moment.tzinfo is not None:
        moment = moment.astimezone(dt.timezone.utc)
    midnight = dt.datetime(moment.year, moment.month, moment.day, tzinfo=dt.timezone.utc)
    micros = (midnight - _EPOCH) // dt.timedelta(microseconds=1)
    return micros * 1000


@dataclass(frozen=True)
class FieldMapping:
    """一行翻译契约：规范层字段 → NT ``Bar()`` 构造参数 + 值变换。

    ``canonical`` 用 SPEC.md drift:ohlcv 表的 json tag 名（跨语言稳定）；
    ``bar_kwarg`` 是 NT ``Bar()`` 的构造参数名；``transform`` 把库里的
    原始值变成 NT 模型对象。
    """

    canonical: str
    bar_kwarg: str
    transform: Callable[[object], object]


#: 规范层 OHLCV → NT ``Bar()`` kwargs 的翻译表。
#:
#: 覆盖关系说明（不是遗漏）：``symbol`` 不进 ``Bar()``——它经
#: :func:`bar_type_for` 决定 bar 类型；``turnover`` / ``trade_days`` /
#: ``frequency`` / ``visible_at`` / ``limit_up`` / ``limit_down`` 在 NT 的
#: Bar 上没有对应参数（NT 侧不感知成交额与停牌标记，规范层字段多于 NT
#: bar 字段是设计使然）。``date`` 占两行：NT ``Bar()`` 同时要 ``ts_event``
#: 与 ``ts_init``，同一规范层字段喂两个构造参数——这是一对多，不是重复。
BAR_FIELD_MAP: tuple[FieldMapping, ...] = (
    FieldMapping("open", "open", as_price),
    FieldMapping("high", "high", as_price),
    FieldMapping("low", "low", as_price),
    FieldMapping("close", "close", as_price),
    FieldMapping("volume", "volume", as_quantity),
    FieldMapping("date", "ts_event", as_ts_utc_midnight_ns),
    FieldMapping("date", "ts_init", as_ts_utc_midnight_ns),
)

#: 翻译表覆盖的规范层字段名（json tag 命名）。
MAPPED_CANONICAL_FIELDS: tuple[str, ...] = tuple(
    dict.fromkeys(m.canonical for m in BAR_FIELD_MAP)
)


def bar_kwargs_for_row(row: Mapping[str, object]) -> dict[str, object]:
    """把一行规范层 OHLCV（列名 → 值）翻译成 NT ``Bar()`` 的 kwargs。

    行里缺翻译表要求的字段 → ``KeyError``（fail-loud，不静默跳过：
    跳过等于把缺失价格当 0 写进内核）；行里的额外列（如 ``symbol``）
    原样忽略——它们不经 ``Bar()`` 消费。
    """
    kwargs: dict[str, object] = {}
    for mapping in BAR_FIELD_MAP:
        try:
            raw = row[mapping.canonical]
        except KeyError:
            raise KeyError(
                f"mapping: 行数据缺少规范层字段 {mapping.canonical!r}"
                f"（NT Bar() 参数 {mapping.bar_kwarg}）——fail-loud，不静默跳过"
            ) from None
        kwargs[mapping.bar_kwarg] = mapping.transform(raw)
    return kwargs


def bar_type_for(symbol: str, venue: str, freq: str) -> str:
    """规范层频率 → NT bar 类型字符串（``"{symbol}.{venue}-{suffix}"``）。

    未知频率 fail-loud：静默落到默认后缀会让 15m 数据冒充日线进场，
    双跑对齐（K15）立刻失真且极难归因。
    """
    try:
        suffix = BAR_SUFFIX_BY_FREQ[freq]
    except KeyError:
        raise ValueError(
            f"mapping: 频率 {freq!r} 没有 NT 导出路径（已实现：{sorted(BAR_SUFFIX_BY_FREQ)}）"
            "——拒绝静默回退到默认后缀"
        ) from None
    return f"{symbol}.{venue}-{suffix}"
