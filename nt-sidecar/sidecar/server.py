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
"""

from __future__ import annotations

import json
import logging
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from .engine import DEFAULT_VENUE, run_backtest
from .errors import RunSpecError, SidecarError
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

    def __init__(self, address: tuple[str, int], *, catalog_path: Path, venue: str):
        super().__init__(address, _Handler)
        self.catalog_path = catalog_path
        self.venue = venue

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
        if self.path.split("?", 1)[0] == HEALTH_PATH:
            self._send_json(200, {"status": "ok", "kernel": "nautilus"})
            return
        self._send_json(404, {"status": "failed", "error": f"no route for GET {self.path}"})

    def do_POST(self) -> None:  # noqa: N802 — stdlib naming
        if self.path.split("?", 1)[0] != RUN_PATH:
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
        try:
            with _run_lock:
                result = run_backtest(
                    run_spec,
                    catalog_path=str(server.catalog_path),
                    venue=server.venue,
                )
        except SidecarError as exc:
            log.warning("run rejected: %s", exc)
            self._send_json(500, {"status": "failed", "error": str(exc)})
            return
        except Exception as exc:  # noqa: BLE001 — never leak a traceback to the caller
            log.exception("unexpected sidecar failure")
            self._send_json(500, {"status": "failed", "error": f"internal sidecar error: {exc}"})
            return

        self._send_json(200, result)

    # ── helpers ───────────────────────────────────────────────────────────────

    def _read_body(self) -> bytes | None:
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            self._send_json(400, {"status": "failed", "error": "invalid Content-Length"})
            return None
        if length <= 0:
            self._send_json(400, {"status": "failed", "error": "empty request body"})
            return None
        if length > MAX_BODY_BYTES:
            self._send_json(413, {"status": "failed", "error": "request body too large"})
            return None
        return self.rfile.read(length)

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
) -> _Server:
    """Construct the server, refusing any non-loopback bind (ADR-031 D5)."""
    if host not in LOOPBACK_HOSTS:
        raise SidecarError(
            f"refusing to bind {host!r}: the sidecar is loopback-only (ADR-031 D5); "
            f"allowed hosts: {sorted(LOOPBACK_HOSTS)}"
        )
    path = Path(catalog_path).expanduser().resolve()
    return _Server((host, port), catalog_path=path, venue=venue)


def serve_forever(server: _Server) -> None:
    log.info(
        "nt-sidecar listening on http://%s:%d (catalog=%s, venue=%s)",
        server.server_address[0],
        server.server_address[1],
        server.catalog_path,
        server.venue,
    )
    server.serve_forever()
