"""Signal consumption for the weight-replay adapter (K14).

Mirrors the Go ``SignalStrategy`` consumption semantics (K7): a target-weight
signal is effective from the first bar whose timestamp is **>= its ``as_of``**, and
a signal dated after the current bar is *physically invisible* to the adapter.
That "no look-ahead" property is a contract, not a convention — the same
discipline the Go side enforces in ``pkg/strategy/signal_store.go``
(``as_of <= upTo``) and ``rejectFutureSignals``.

``as_of`` is normalised to a UTC timestamp so that both daily signals (midnight)
and intra-day signals can be compared against a bar's ``ts_event`` on one axis.
The adapter only ever asks for ``stream.as_of(bar_timestamp)``; it never indexes
individual signals, so it cannot read a future bar.
"""

from __future__ import annotations

import bisect
import datetime as _dt
from dataclasses import dataclass

_UTC = _dt.timezone.utc


def _to_utc(value: _dt.date | _dt.datetime | str) -> _dt.datetime:
    """Normalise a date / datetime / ISO string to an aware UTC datetime."""
    if isinstance(value, _dt.datetime):
        moment = value
    elif isinstance(value, _dt.date):
        moment = _dt.datetime(value.year, value.month, value.day)
    elif isinstance(value, str):
        moment = _dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    else:
        raise ValueError(f"cannot parse as_of from {value!r}")
    if moment.tzinfo is None:
        moment = moment.replace(tzinfo=_UTC)
    return moment.astimezone(_UTC)


@dataclass(frozen=True)
class TargetWeight:
    """A target portfolio weight for ``symbol``, effective from ``as_of``."""

    symbol: str
    weight: float
    as_of: _dt.date | _dt.datetime | str

    def __post_init__(self) -> None:
        if not self.symbol:
            raise ValueError("target weight: symbol is required")
        if not 0.0 <= self.weight <= 1.0:
            raise ValueError(f"target weight must be in [0, 1], got {self.weight}")
        # Normalise so date and datetime signals share one comparison axis.
        object.__setattr__(self, "as_of", _to_utc(self.as_of))


def parse_date(value: object) -> _dt.date:
    """Parse a date-like value to a ``datetime.date`` (midnight-normalised)."""
    return _to_utc(value).date()  # type: ignore[arg-type]


class SignalStream:
    """An append-only, as-of-ordered stream of :class:`TargetWeight` signals."""

    def __init__(self, signals: list[TargetWeight] | tuple[TargetWeight, ...] = ()) -> None:
        self._signals = sorted(signals, key=lambda s: (s.as_of, s.symbol))
        # K14-R1 F · the sorted as_of keys let as_of()/target_weight() bound their
        # scan with a bisect instead of a full O(N) sweep on every bar.
        self._as_of = [s.as_of for s in self._signals]

    @classmethod
    def from_rows(cls, rows: list[dict]) -> "SignalStream":
        """Build from raw rows (e.g. parsed from ``quant.external_signals``)."""
        return cls(
            [
                TargetWeight(
                    symbol=str(row["symbol"]),
                    weight=float(row["weight"]),
                    as_of=row["as_of"],
                )
                for row in rows
            ]
        )

    def __len__(self) -> int:
        return len(self._signals)

    def _prefix_end(self, cutoff: _dt.datetime) -> int:
        """Index just past every signal with ``as_of <= cutoff`` (sorted prefix)."""
        return bisect.bisect_right(self._as_of, cutoff)

    def as_of(self, moment: _dt.date | _dt.datetime | str) -> dict[str, TargetWeight]:
        """Latest target weight **per symbol** with ``as_of <= moment``.

        Future signals (``as_of > moment``) are never returned — the
        no-look-ahead contract. When several signals share a symbol, the latest
        ``as_of`` wins.
        """
        cutoff = _to_utc(moment)
        latest: dict[str, TargetWeight] = {}
        for i in range(self._prefix_end(cutoff)):
            signal = self._signals[i]
            latest[signal.symbol] = signal
        return latest

    def target_weight(self, symbol: str, moment: _dt.date | _dt.datetime | str) -> float | None:
        """Target weight for one symbol as of ``moment``, or ``None`` if unset."""
        cutoff = _to_utc(moment)
        # Scan the eligible prefix backwards: the first match is the latest as_of
        # for that symbol, so we exit early instead of building the whole map.
        for i in range(self._prefix_end(cutoff) - 1, -1, -1):
            signal = self._signals[i]
            if signal.symbol == symbol:
                return signal.weight
        return None


def sample_signals(
    symbol: str,
    *,
    enter: _dt.date | _dt.datetime,
    exit_on: _dt.date | _dt.datetime,
    entry_weight: float = 0.5,
) -> SignalStream:
    """A minimal, deterministic sample signal stream (K14 最小样本策略).

    Hold ``entry_weight`` from ``enter`` (inclusive) until ``exit_on``, then flat.
    No CLI, no randomness — the point is a reproducible input the adapter's
    consistency gate can replay.
    """
    return SignalStream(
        [
            TargetWeight(symbol=symbol, weight=entry_weight, as_of=enter),
            TargetWeight(symbol=symbol, weight=0.0, as_of=exit_on),
        ]
    )
