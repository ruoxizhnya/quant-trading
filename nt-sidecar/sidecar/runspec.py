"""The RunSpec wire DTO (K13).

The field names deliberately mirror the Go ``contracts.BacktestRequest`` so the
mapping in ``pkg/backtest/nautilus/runner.go`` is a straight field copy and the
sidecar never has to know about kernel-specific Go types.

Validation is fail-closed, matching the Go side's ``ValidateRunSpec`` idiom:
anything the sidecar does not understand is **rejected, not ignored**, so a
caller can never believe a parameter took effect when it did not.
"""

from __future__ import annotations

import datetime as _dt
import math
from dataclasses import dataclass, field
from typing import Any

from .errors import RunSpecError

_DATE_FMT = "%Y-%m-%d"

#: The only kernel this sidecar serves. The Go registry routes ``kernel="nautilus"``
#: here; any other value reaching this process is a wiring bug, so it is refused.
SERVED_KERNEL = "nautilus"

DEFAULT_INITIAL_CAPITAL = 1_000_000.0

#: Default per-day participation cap (K14): traded shares ≤ D_max × bar volume.
#: Kept as a plain literal here so this module stays importable without the
#: (heavy) NautilusTrader stack; mirrored by ``weight_replay.DEFAULT_D_MAX``.
DEFAULT_D_MAX = 0.10


@dataclass(frozen=True)
class RunSpec:
    """A validated backtest request as sent by the Go ``NautilusRunner``."""

    strategy: str
    stock_pool: tuple[str, ...]
    start_date: str
    end_date: str
    kernel: str = SERVED_KERNEL
    index_code: str = ""
    initial_capital: float = DEFAULT_INITIAL_CAPITAL
    risk_free_rate: float = 0.0
    d_max: float = DEFAULT_D_MAX
    engine_params: dict[str, Any] = field(default_factory=dict)
    #: K15 · optional target-weight signal rows, straight from the caller (the
    #: Go ``NautilusRunner`` serialises ``contracts.BacktestRequest.Signals``).
    #: Each row is a validated ``{"symbol", "weight", "as_of"}`` dict. Empty
    #: tuple = "not provided" → the weight-replay adapter falls back to its
    #: built-in deterministic sample stream (K14 behaviour, unchanged).
    signals: tuple[dict[str, Any], ...] = ()

    @classmethod
    def from_json(cls, payload: Any) -> "RunSpec":
        if not isinstance(payload, dict):
            raise RunSpecError("run spec must be a JSON object")

        strategy = _require_str(payload, "strategy")

        raw_pool = payload.get("stock_pool")
        if raw_pool is None:
            raw_pool = []
        if isinstance(raw_pool, str):
            raw_pool = [raw_pool]
        if not isinstance(raw_pool, (list, tuple)):
            raise RunSpecError("run spec: 'stock_pool' must be a list of symbols")
        pool = tuple(s for s in (str(item).strip() for item in raw_pool) if s)
        if not pool:
            raise RunSpecError("run spec: 'stock_pool' must contain at least one symbol")

        start_date = _require_date(payload, "start_date")
        end_date = _require_date(payload, "end_date")
        if _dt.date.fromisoformat(start_date) > _dt.date.fromisoformat(end_date):
            raise RunSpecError(
                f"run spec: start_date {start_date} is after end_date {end_date}"
            )

        kernel = str(payload.get("kernel") or SERVED_KERNEL)
        if kernel != SERVED_KERNEL:
            raise RunSpecError(
                f"run spec: kernel {kernel!r} is not served by this sidecar "
                f"(only {SERVED_KERNEL!r})"
            )

        params = payload.get("engine_params")
        if params is None:
            params = {}
        if not isinstance(params, dict):
            raise RunSpecError("run spec: 'engine_params' must be an object")
        if params:
            # K13: the nautilus kernel declares no engine params yet, so an
            # unknown key is rejected rather than silently dropped. Mirrors the
            # Go side, where KnownEngineParams("nautilus") is the empty set.
            raise RunSpecError(
                "run spec: kernel 'nautilus' accepts no engine_params yet "
                f"(rejected, not ignored): got {sorted(params)}"
            )

        capital = _parse_capital(payload)
        d_max = _parse_d_max(payload)
        signals = _parse_signals(payload)

        risk_free = payload.get("risk_free_rate") or 0.0

        return cls(
            strategy=strategy,
            stock_pool=pool,
            start_date=start_date,
            end_date=end_date,
            kernel=kernel,
            index_code=str(payload.get("index_code") or ""),
            initial_capital=capital,
            risk_free_rate=float(risk_free),
            d_max=d_max,
            engine_params=dict(params),
            signals=signals,
        )


def _parse_capital(payload: dict[str, Any]) -> float:
    """Parse and fail-closed-validate ``initial_capital`` (K13-R1 C).

    ``None``/absent means "not specified" → the documented default. Anything
    else must be a **finite positive** number:

    * ``inf`` / ``nan`` pass a naive ``> 0`` check downstream (``inf > 0`` is
      true) and then blow up inside the engine's ``int()`` → a 500 that should
      have been a 400. Rejected here instead.
    * an explicit ``0`` is a *reject-not-ignore* failure, not a silent "use the
      default" — matching the Go side's fail-closed idiom. (Previously ``0`` was
      truthy-falsy short-circuited into the default, which hid a caller bug.)
    """
    raw = payload.get("initial_capital")
    if raw is None:
        return DEFAULT_INITIAL_CAPITAL
    try:
        capital = float(raw)
    except (TypeError, ValueError) as exc:
        raise RunSpecError(
            f"run spec: 'initial_capital' must be a number, got {raw!r}"
        ) from exc
    if not math.isfinite(capital) or capital <= 0:
        raise RunSpecError(
            f"run spec: 'initial_capital' must be a finite positive number, got {raw!r}"
        )
    return capital


def _parse_d_max(payload: dict[str, Any]) -> float:
    """Parse the optional per-day participation cap (K14).

    Absent/``None`` → the documented default. Anything else must be a finite
    number in ``(0, 1]`` (a fraction of the bar's volume); a value outside that
    range is a caller bug and is rejected, not clamped.
    """
    raw = payload.get("d_max")
    if raw is None:
        return DEFAULT_D_MAX
    try:
        value = float(raw)
    except (TypeError, ValueError) as exc:
        raise RunSpecError(f"run spec: 'd_max' must be a number, got {raw!r}") from exc
    if not math.isfinite(value) or not 0.0 < value <= 1.0:
        raise RunSpecError(
            f"run spec: 'd_max' must be a finite fraction in (0, 1], got {value!r}"
        )
    return value


def _parse_signals(payload: dict[str, Any]) -> tuple[dict[str, Any], ...]:
    """Parse and fail-closed-validate the optional ``signals`` rows (K15).

    Each row must be ``{"symbol": str, "weight": float, "as_of": "YYYY-MM-DD"}``:
    a non-blank symbol, a finite weight in ``[0, 1]``, and an ISO date. Anything
    else is a caller bug — rejected, not coerced. The same discipline as
    :class:`sidecar.signals.TargetWeight` ``__post_init__``, applied at the wire
    so a malformed row can never reach the adapter mid-run.
    """
    raw = payload.get("signals")
    if raw is None:
        return ()
    if not isinstance(raw, list):
        raise RunSpecError("run spec: 'signals' must be a list of rows")
    rows: list[dict[str, Any]] = []
    for i, item in enumerate(raw):
        if not isinstance(item, dict):
            raise RunSpecError(f"run spec: signals[{i}] must be an object, got {item!r}")
        symbol = item.get("symbol")
        if not isinstance(symbol, str) or not symbol.strip():
            raise RunSpecError(f"run spec: signals[{i}].symbol must be a non-blank string")
        weight = item.get("weight")
        try:
            weight_f = float(weight)
        except (TypeError, ValueError) as exc:
            raise RunSpecError(
                f"run spec: signals[{i}].weight must be a number, got {weight!r}"
            ) from exc
        if not math.isfinite(weight_f) or not 0.0 <= weight_f <= 1.0:
            raise RunSpecError(
                f"run spec: signals[{i}].weight must be a finite fraction in [0, 1], got {weight_f!r}"
            )
        as_of = item.get("as_of")
        if not isinstance(as_of, str) or not as_of.strip():
            raise RunSpecError(f"run spec: signals[{i}].as_of must be an ISO date string")
        text = as_of.strip()
        try:
            parsed = _dt.datetime.strptime(text, _DATE_FMT)
        except ValueError as exc:
            raise RunSpecError(
                f"run spec: signals[{i}].as_of must be an ISO date (YYYY-MM-DD), got {as_of!r}"
            ) from exc
        # Round-trip guard: strptime is lenient about zero padding ("2024-1-2"
        # parses fine), but the Go side's time.Parse rejects it — the two
        # validators must agree on exactly the same set of accepted rows, or a
        # request can be "Go accepts, sidecar accepts" today and silently
        # diverge when the call order changes.
        if parsed.strftime(_DATE_FMT) != text:
            raise RunSpecError(
                f"run spec: signals[{i}].as_of must be a zero-padded ISO date (YYYY-MM-DD), got {as_of!r}"
            )
        rows.append(
            {"symbol": symbol.strip(), "weight": weight_f, "as_of": text}
        )
    return tuple(rows)


def _require_str(payload: dict[str, Any], key: str) -> str:
    value = payload.get(key)
    if not isinstance(value, str) or not value.strip():
        raise RunSpecError(f"run spec: {key!r} is required")
    return value.strip()


def _require_date(payload: dict[str, Any], key: str) -> str:
    value = _require_str(payload, key)
    try:
        _dt.datetime.strptime(value, _DATE_FMT)
    except ValueError as exc:
        raise RunSpecError(
            f"run spec: {key!r} must be an ISO date (YYYY-MM-DD), got {value!r}"
        ) from exc
    return value
