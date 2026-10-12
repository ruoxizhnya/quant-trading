"""NT sidecar (K13): a minimal, loopback-only RunSpec execution service.

Layering:

* ``sidecar.errors``     — error taxonomy (RunSpec errors vs engine errors)
* ``sidecar.runspec``    — the wire DTO + fail-closed validation
* ``sidecar.catalog``    — symbol -> NautilusTrader instrument / bar-type mapping
* ``sidecar.strategies`` — demo strategies (K13 ships a buy-and-hold only)
* ``sidecar.engine``     — the NautilusTrader BacktestNode driver + result mapping
* ``sidecar.server``     — the HTTP/JSON transport (127.0.0.1 only)

Nothing in this package connects to a database: ADR-031 D5 requires the sidecar
to receive everything through the RunSpec and the pre-materialised Parquet
catalog.
"""

from .errors import RunSpecError, SidecarError

__all__ = ["RunSpecError", "SidecarError", "__version__"]

__version__ = "0.1.0"
