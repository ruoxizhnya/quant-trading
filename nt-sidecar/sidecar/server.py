"""HTTP/JSON transport for the NT sidecar (K13).

Transport decision (design doc §8, item 2): **HTTP/1.1 + JSON over loopback**,
implemented with the standard library so the only runtime dependencies are
NautilusTrader and its data stack. Rationale and the switch trigger live in the
design doc; in short, one synchronous request/response endpoint is all K13
needs, and gRPC would add a protobuf toolchain and codegen on both ends for no
benefit yet.

Security posture (ADR-031 D5): the process refuses to bind anything other than a
loopback address, has no database access, and is reachable only by the Go
service on the same host.

K13-R1 hardening: early-exit paths (404 / 413 / invalid Content-Length) either
consume the request body or force the connection closed, so a keep-alive client
can never have its next request desynced by leftover body bytes. A successful
run also reports ``timing`` (queue_ms / exec_ms / total_ms) for the client's
deadline phase attribution.
"""

from __future__ import annotations

import json
import logging
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from .engine import DEFAULT_VENUE, run_backtest
from .errors import RunSpecError, SidecarError
from .markets import DEFAULT_MARKET, resolve_market
from .runspec import RunSpec

#: 1 MiB is far above any RunSpec and keeps a hostile client from exhausting memory.
MAX_BODY_BYTES = 1 << 20

#: Loopback names the server is willing to bind. Anything else is refused.
LOOPBACK_HOSTS = frozenset({"127.0.0.1", "localhost", "::1"})

RUN_PATH = "/v1/backtest"
HEALTH_PATH = "/healthz"

log = logging.getLogger("nt-sidecar")

#: NautilusTrader runs are serialised. The engine is CPU- and memory-heavy and
#: K13 has no concurrency requirement; one run at a time keeps behaviour
#: deterministic (a property the K15 double-run alignment experiment needs).
_run_lock = threading.Lock()


class _Server(ThreadingHTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(
        self,
        address: tuple[str, int],
        *,
        catalog_path: Path,
        venue: str,
        market: str = DEFAULT_MARKET,
    ):
        super().__init__(address, _Handler)
        self.catalog_path = catalog_path
        self.venue = venue
        self.market = market

    def handle_error(self, request, client_address) -> None:  # noqa: N802 — stdlib naming
        # A client that closes the socket before reading the response (curl
        # without keep-alive, a cancelled request) is normal traffic, not a
        # server fault — do not dump a traceback for it.
        exc = sys.exc_info()[1]
        if isinstance(exc, (ConnectionResetError, ConnectionAbortedError, BrokenPipeError)):
            log.debug("client %s disconnected mid-request: %s", client_address, exc)
            return
        log.exception("unhandled error while serving %s", client_address)


class _Handler(BaseHTTPRequestHandler):
    server_version = "nt-sidecar/0.1.0"
    protocol_version = "HTTP/1.1"

    # ── routing ───────────────────────────────────────────────────────────────

    def do_GET(self) -> None:  # noqa: N802 — stdlib naming
        # K13-R1-R1 B: neither GET route reads a request body, so a client that
        # (rarely) sends one would leave the bytes unread and desync a keep-alive
        # connection — the same failure K13-R1 B fixed for POST's 404 path.
        # Drain it; if we can't, force the connection closed.
        if not self._drain_body():
            self.close_connection = True
        if self.path.split("?", 1)[0] == HEALTH_PATH:
            self._send_json(200, {"status": "ok", "kernel": "nautilus"})
            return
        self._send_json(404, {"status": "failed", "error": f"no route for GET {self.path}"})

    def do_POST(self) -> None:  # noqa: N802 — stdlib naming
        if self.path.split("?", 1)[0] != RUN_PATH:
            # K13-R1 B: don't answer before consuming the request body, or the
            # leftover bytes desync a keep-alive connection (they get parsed as
            # the next request line). Drain it; if we can't (oversized/invalid
            # Content-Length), force the connection closed instead.
            if not self._drain_body():
                self.close_connection = True
            self._send_json(404, {"status": "failed", "error": f"no route for POST {self.path}"})
            return
        self._handle_run()

    # ── handlers ──────────────────────────────────────────────────────────────

    def _handle_run(self) -> None:
        raw = self._read_body()
        if raw is None:
            return

        try:
            payload = json.loads(raw)
        except json.JSONDecodeError as exc:
            self._send_json(400, {"status": "failed", "error": f"invalid JSON body: {exc}"})
            return

        try:
            run_spec = RunSpec.from_json(payload)
        except RunSpecError as exc:
            self._send_json(400, {"status": "failed", "error": str(exc)})
            return

        server: _Server = self.server  # type: ignore[assignment]
        # K13-R1 增项 · deadline 五段拆：report the two server-side phases the
        # client cannot observe — queue (time waiting on the serialising run
        # lock) and exec (the engine run itself). The client pairs these with
        # its own Call+Queue / Transfer+Process measurements.
        started = time.perf_counter()
        try:
            with _run_lock:
                acquired = time.perf_counter()
                result = run_backtest(
                    run_spec,
                    catalog_path=str(server.catalog_path),
                    venue=server.venue,
                    market=server.market,
                )
                finished = time.perf_counter()
        except SidecarError as exc:
            log.warning("run rejected: %s", exc)
            self._send_json(500, {"status": "failed", "error": str(exc)})
            return
        except Exception as exc:  # noqa: BLE001 — never leak a traceback to the caller
            log.exception("unexpected sidecar failure")
            self._send_json(500, {"status": "failed", "error": f"internal sidecar error: {exc}"})
            return

        result["timing"] = {
            "queue_ms": round((acquired - started) * 1000.0, 3),
            "exec_ms": round((finished - acquired) * 1000.0, 3),
            "total_ms": round((finished - started) * 1000.0, 3),
        }
        self._send_json(200, result)

    # ── helpers ───────────────────────────────────────────────────────────────

    def _read_body(self) -> bytes | None:
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            # K13-R1 B: we cannot know how many bytes to skip, so close the
            # connection rather than let the unread body desync the next request.
            self.close_connection = True
            self._send_json(400, {"status": "failed", "error": "invalid Content-Length"})
            return None
        if length <= 0:
            self._send_json(400, {"status": "failed", "error": "empty request body"})
            return None
        if length > MAX_BODY_BYTES:
            # Not draining an oversized body on purpose; close the connection so
            # the residual bytes are not parsed as the next request line.
            self.close_connection = True
            self._send_json(413, {"status": "failed", "error": "request body too large"})
            return None
        return self.rfile.read(length)

    def _drain_body(self) -> bool:
        """Read and discard the request body (K13-R1 B).

        Returns ``False`` when the body cannot be safely drained — an
        unparseable or oversized ``Content-Length``. The caller must then set
        ``self.close_connection = True`` so the residual bytes are not parsed as
        the next request on a kept-alive connection.
        """
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            return False
        if length <= 0:
            return True
        if length > MAX_BODY_BYTES:
            return False
        self.rfile.read(length)
        return True

    def _send_json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, allow_nan=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args: Any) -> None:  # noqa: A003 — stdlib naming
        log.info("%s - %s", self.address_string(), fmt % args)


def build_server(
    *,
    host: str = "127.0.0.1",
    port: int = 9100,
    catalog_path: str | Path,
    venue: str = DEFAULT_VENUE,
    market: str = DEFAULT_MARKET,
) -> _Server:
    """Construct the server, refusing any non-loopback bind (ADR-031 D5).

    The **market** is resolved here, at assembly time (D7): an unregistered
    market makes the process refuse to start rather than serve wrong rules at
    run time — the same fail-closed shape as ``NT_SIDECAR_ENABLED``'s typo
    guard, one layer down.
    """
    if host not in LOOPBACK_HOSTS:
        raise SidecarError(
            f"refusing to bind {host!r}: the sidecar is loopback-only (ADR-031 D5); "
            f"allowed hosts: {sorted(LOOPBACK_HOSTS)}"
        )
    profile = resolve_market(market)
    path = Path(catalog_path).expanduser().resolve()
    return _Server(
        (host, port), catalog_path=path, venue=venue, market=profile.market
    )


def serve_forever(server: _Server) -> None:
    log.info(
        "nt-sidecar listening on http://%s:%d (catalog=%s, venue=%s, market=%s)",
        server.server_address[0],
        server.server_address[1],
        server.catalog_path,
        server.venue,
        server.market,
    )
    server.serve_forever()
