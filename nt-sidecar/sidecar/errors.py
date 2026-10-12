"""Error taxonomy for the NT sidecar (K13).

Two failure classes, because they map to different HTTP statuses:

* :class:`RunSpecError` — the caller's RunSpec is malformed or asks for
  something this sidecar does not serve. Mapped to **400**.
* :class:`SidecarError`  — the request was well formed but the engine could not
  complete it (missing catalog data, NautilusTrader raised, no run result).
  Mapped to **500**.

Both are surfaced to the Go caller as an explicit failure; the Go
``nautilus.Runner`` never turns them into a synthesised success (see
``pkg/backtest/nautilus/runner.go`` — fail-closed, "no silent fallback").
"""


class SidecarError(Exception):
    """The engine could not complete an otherwise valid request."""


class RunSpecError(SidecarError):
    """The incoming RunSpec is malformed or unsupported (HTTP 400)."""
