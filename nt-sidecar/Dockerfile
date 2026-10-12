# K13 / K13-R1 · NautilusTrader sidecar image (OPTIONAL sample).
#
# The sidecar is normally run as a co-located process on the host (`python -m
# sidecar`, see README.md), which is what the ADR-031 D5 loopback-only bind
# assumes. This image exists so the same process can also be launched from
# compose — see docker-compose.nt.yaml for the networking caveat.
#
# Pin matches nt-sidecar/requirements.txt (NautilusTrader 2.0.0rc6, cp313 wheel).
FROM python:3.13-slim

WORKDIR /app

# The scientific stack NT pulls (numpy/pandas/pyarrow) needs a C toolchain-free
# wheel-only install; --no-cache keeps the layer small.
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# Service code only — no DB access (ADR-031 D5): tools/ is copied for the
# offline export step, but the running service never imports psycopg.
COPY sidecar ./sidecar
COPY tools ./tools
COPY README.md ./

# The catalog (Parquet, produced offline by tools/export_bars.py) is mounted
# read-only; it is never written by the service.
VOLUME ["/catalog"]

# In-container bind stays on loopback (ADR-031 D5) — the process refuses any
# other host. Reaching it across containers therefore requires host networking.
EXPOSE 9100
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://127.0.0.1:9100/healthz', timeout=3).status == 200 else 1)"

ENTRYPOINT ["python", "-m", "sidecar"]
CMD ["--host", "127.0.0.1", "--port", "9100", "--catalog", "/catalog"]
