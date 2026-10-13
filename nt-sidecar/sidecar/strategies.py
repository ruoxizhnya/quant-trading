"""Strategies the sidecar can serve (K13 demo + K14 weight-replay).

K13 validates the wire with a minimal buy-and-hold demo. K14 adds the real
adapter — ``weight_replay`` — which consumes a target-weight signal stream under
the A-share minimal rule set (:mod:`sidecar.weight_replay`). The K14 sample
strategy generates its **own** deterministic signal stream from the run window
(no CLI, no external signal source): the point is a reproducible sample link,
not a signal-delivery mechanism.
"""

from __future__ import annotations

from datetime import date
from decimal import Decimal

from nautilus_trader.config import StrategyConfig
from nautilus_trader.model import Bar, BarType, InstrumentId, OrderSide
from nautilus_trader.trading import Strategy

from .errors import SidecarError
from .signals import SignalStream, sample_signals
from .weight_replay import DEFAULT_D_MAX, WeightReplay, WeightReplayConfig

#: Default notional-per-run trade size for the demo (shares).
DEFAULT_TRADE_SIZE = Decimal(10_000)


class BuyAndHoldConfig(StrategyConfig):
    """Config for :class:`BuyAndHold`.

    v2 ``StrategyConfig`` is a PyO3 type: custom fields are declared as
    keyword-only arguments, the surplus ``**_kwargs`` is forwarded untouched so
    the base class can read its own fields, and ``super().__init__()`` is called
    with no arguments.
    """

    def __init__(
        self,
        *,
        instrument_id: InstrumentId,
        bar_type: BarType,
        trade_size: Decimal,
        **_kwargs: object,
    ) -> None:
        super().__init__()
        self.instrument_id = instrument_id
        self.bar_type = bar_type
        self.trade_size = trade_size


class BuyAndHold(Strategy):
    """Enter once on the first bar, then do nothing for the rest of the window."""

    def __init__(self, config: BuyAndHoldConfig) -> None:
        super().__init__(config)
        self._entered = False

    def on_start(self) -> None:
        self.subscribe_bars(self.config.bar_type)

    def on_bar(self, bar: Bar) -> None:
        if self._entered:
            return
        self._entered = True
        instrument = self.cache.instrument(self.config.bar_type.instrument_id)
        if instrument is None:
            self.log.error(
                f"instrument {self.config.bar_type.instrument_id} missing from cache; "
                "no order submitted"
            )
            return
        order = self.order_factory.market(
            self.config.bar_type.instrument_id,
            OrderSide.BUY,
            instrument.make_qty(self.config.trade_size),
        )
        self.submit_order(order)


#: Registry of demo strategies the sidecar will serve. K14 adds the real one.
DEMO_STRATEGIES: dict[str, tuple[type[Strategy], type[StrategyConfig]]] = {
    "buy_and_hold": (BuyAndHold, BuyAndHoldConfig),
}

#: Strategy name that selects the K14 weight-replay adapter.
WEIGHT_REPLAY = "weight_replay"


def build_strategy(
    name: str,
    *,
    instrument_id: InstrumentId,
    bar_type: BarType,
    trade_size: Decimal = DEFAULT_TRADE_SIZE,
    start_date: str | None = None,
    end_date: str | None = None,
    initial_capital: float = 1_000_000.0,
    d_max: float = DEFAULT_D_MAX,
    signal_rows: tuple[dict, ...] = (),
) -> Strategy:
    """Build the strategy named ``name``.

    ``buy_and_hold`` — the K13 demo (fixed-size entry on the first bar).
    ``weight_replay`` — the K14 adapter. When ``signal_rows`` are provided
    (K15: caller-supplied target-weight stream, mirroring
    ``quant.external_signals`` row shape), they are consumed verbatim; otherwise
    the built-in deterministic sample stream spans the run window (50% entry,
    flat at the window end) — the K14 behaviour, unchanged.
    """
    if name == WEIGHT_REPLAY:
        if not start_date or not end_date:
            raise SidecarError("weight_replay requires start_date and end_date")
        symbol = instrument_id.symbol.value
        if signal_rows:
            stream = SignalStream.from_rows(list(signal_rows))
        else:
            stream = sample_signals(
                symbol,
                enter=date.fromisoformat(start_date),
                exit_on=date.fromisoformat(end_date),
                entry_weight=0.5,
            )
        return WeightReplay(
            WeightReplayConfig(
                instrument_id=instrument_id,
                bar_type=bar_type,
                signals=stream,
                capital_base=initial_capital,
                d_max=d_max,
            )
        )

    try:
        strategy_cls, config_cls = DEMO_STRATEGIES[name]
    except KeyError:
        raise SidecarError(
            f"unknown strategy {name!r}; known strategies: "
            f"{sorted([*DEMO_STRATEGIES, WEIGHT_REPLAY])}"
        ) from None
    config = config_cls(
        instrument_id=instrument_id,
        bar_type=bar_type,
        trade_size=trade_size,
    )
    return strategy_cls(config)


#: Backwards-compatible alias (K13 call sites / tests).
build_demo_strategy = build_strategy
