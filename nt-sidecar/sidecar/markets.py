"""市场档案（market profiles）—— NT 原生模式的配置面（D7）。

两层配置，各管一段，不要混：

* **路由侧（Go）**：`kernel_routing.markets` 决定「哪个市场由哪个内核跑」
  （`CN → go` / `US → nautilus`），未注册市场 400。见
  `pkg/backtest/contracts/marketroute.go`。
* **执行侧（本模块）**：NT 内核跑某个市场时用哪套**原生**规则——计价币种、
  精度、费率模型、执行模式。

为什么执行侧也必须是「档案」而不是 if/else：设计蓝图 §2.1 把「热转换」钉成
**配置级**——新市场接入 = 加一条配置 + 对应 connector，不改路由/执行代码。
「加一个市场 = 加一条档案」是这条语义在执行侧的落地。

两个档案：

* ``CN`` —— A 股线。行为与 K13–K16 **逐字一致**（权重回放适配器 + ashare
  最小规则集），D7 不动它（ADR-031「不推翻既有裁决」）。
* ``US`` —— 国外线 **NT 原生模式**。USD 计价、NT 自带订单类型与执行模型，
  **不做 A 股规则魔改**（无 T+1、无涨跌停、无整手）——把 A 股规则套到美股上
  是「魔改」，正是本档案要避免的事。

未知市场 fail-closed（拒绝而非回退 CN）：静默按 A 股规则跑，回执看起来完全
正常，与 FR-1.3「不得静默回退内核」是同一个病，只是发生在市场维度。
"""

from __future__ import annotations

from dataclasses import dataclass

from .errors import MarketError

#: 缺省市场 = A 股线。默认值保证存量部署（不带 ``--market``）行为不变。
DEFAULT_MARKET = "CN"

#: 执行模式词表。``weight_replay`` = A 股权重回放适配器（K14，不动）；
#: ``native`` = NT 原生执行通道（D7 新增）。
EXECUTION_WEIGHT_REPLAY = "weight_replay"
EXECUTION_NATIVE = "native"

#: 费率模型词表。``ashare`` = sidecar.ashare 的 A 股费率（与 Go 侧 parity）；
#: ``native`` = NT 自带模型（``MakerTakerFeeModel``，样例期零费率）。
FEE_MODEL_ASHARE = "ashare"
FEE_MODEL_NATIVE = "native"

#: 执行模式 / 费率模型的封闭词表——档案写错词在装配期就炸，不留到运行期。
KNOWN_EXECUTION_MODES: tuple[str, ...] = (EXECUTION_WEIGHT_REPLAY, EXECUTION_NATIVE)
KNOWN_FEE_MODELS: tuple[str, ...] = (FEE_MODEL_ASHARE, FEE_MODEL_NATIVE)


@dataclass(frozen=True)
class MarketProfile:
    """一条市场档案：NT 内核跑这个市场时的原生规则参数。"""

    market: str
    #: 计价币种（NT ``Currency`` 代码）。
    currency: str
    #: 价格精度与最小变动单位（``Price(value, precision=...)`` 的构造参数）。
    price_precision: int
    price_increment: str
    fee_model: str
    execution_mode: str


#: 市场档案表。**加一个市场 = 加一行**（与 mapping.BAR_SUFFIX_BY_FREQ 同款机制）。
MARKET_PROFILES: dict[str, MarketProfile] = {
    "CN": MarketProfile(
        market="CN",
        currency="CNY",
        price_precision=2,
        price_increment="0.01",
        fee_model=FEE_MODEL_ASHARE,
        execution_mode=EXECUTION_WEIGHT_REPLAY,
    ),
    "US": MarketProfile(
        market="US",
        currency="USD",
        price_precision=2,
        price_increment="0.01",
        fee_model=FEE_MODEL_NATIVE,
        execution_mode=EXECUTION_NATIVE,
    ),
}


def known_markets() -> tuple[str, ...]:
    """已登记市场名的排序副本（诊断 / 装配日志 / 错误消息用）。"""
    return tuple(sorted(MARKET_PROFILES))


def resolve_market(name: str) -> MarketProfile:
    """把市场名解析成档案；未登记 → :class:`MarketError`（拒绝而非回退）。

    错误消息点名三件事：被拒的市场名、已登记市场集合、以及「拒绝而非回退」
    的语义——与 ``contracts.EngineRegistry.Resolve`` 的错误形状刻意对齐，
    运维在两处看到的是同一种句式。
    """
    key = (name or "").strip().upper()
    if not key:
        raise MarketError(
            f"market {name!r} is empty; refusing to guess — registered markets: "
            f"[{', '.join(known_markets())}] (no fallback to {DEFAULT_MARKET})"
        )
    try:
        return MARKET_PROFILES[key]
    except KeyError:
        raise MarketError(
            f"market {key!r} is not served by this sidecar (registered markets: "
            f"[{', '.join(known_markets())}]); request refused — no silent fallback "
            f"to the default {DEFAULT_MARKET} market"
        ) from None


def validate_profile(profile: MarketProfile) -> None:
    """装配期自检：档案里的词表取值必须封闭（写错字 fail-fast）。

    ``MARKET_PROFILES`` 是手工维护的表；一个拼错的 ``execution_mode`` 会让
    该市场在运行期落到默认分支。装配期炸掉好过运行期悄悄跑错规则。
    """
    if profile.execution_mode not in KNOWN_EXECUTION_MODES:
        raise MarketError(
            f"market {profile.market!r}: unknown execution_mode "
            f"{profile.execution_mode!r} (known: {list(KNOWN_EXECUTION_MODES)})"
        )
    if profile.fee_model not in KNOWN_FEE_MODELS:
        raise MarketError(
            f"market {profile.market!r}: unknown fee_model {profile.fee_model!r} "
            f"(known: {list(KNOWN_FEE_MODELS)})"
        )
