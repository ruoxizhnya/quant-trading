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


class MarketError(SidecarError):
    """A market (or market profile) this sidecar does not serve (HTTP 400).

    D7: the sidecar serves one **market profile** per deployment
    (``--market``). An unknown market must be refused at assembly time, never
    silently served with another market's rule set — running a US request under
    the A-share profile would produce a fully plausible-looking receipt computed
    with the wrong rules, which is exactly the failure mode FR-1.3 forbids for
    kernels, one level down.
    """
