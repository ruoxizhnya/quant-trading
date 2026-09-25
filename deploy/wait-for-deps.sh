#!/bin/sh
# 等外部依赖可达后再启动服务（「数据库跑宿主机、服务跑容器」形态）。
#
# 为什么需要它：应用连不上 PG / Redis 时是 logger.Fatal() 直接退出，**没有重试**
# （cmd/data/setup.go:137「Failed to connect to PostgreSQL」/ :146「Failed to
# connect to Redis」）。此前这条启动顺序由 compose 的
# `depends_on: {postgres: {condition: service_healthy}}` 保证 —— 当 postgres /
# redis 不再是 compose 成员，那个保证就消失了。
#
# **这里是替换，不是删除**：把「等依赖健康」从 compose 编排改到容器内部，
# 而且比原来更严格 —— 原来只等 compose 服务，现在等的是宿主机上的真实端口。
#
# 用法（compose 的 entrypoint + command）：
#   entrypoint: ["/bin/sh", "/app/wait-for-deps.sh"]
#   command: ["/app/data-service"]
#
# ── 等什么：从**应用自己的配置**推导，不另维护一份依赖清单 ──────────────
#
#   DATABASE_HOST + DATABASE_PORT  →  等 <host>:<port>
#   REDIS_URL                      →  去 scheme 后等 <host>:<port>
#
# 这样「等的」与「连的」在结构上不可能不一致。
#
# ⚠️ 这里踩过一次（2026-09-25，被 docker compose up 实测抓出来）：最初写成让
# compose 传参 `postgres:5432 redis:6379` —— 那是**旧的 compose 服务名**，
# 服务移出 compose 后这两个名字已经不存在，于是容器卡在 DNS 解析里空转、
# 日志一片空白、healthcheck 超时判 unhealthy。改成上面这条推导规则后，
# 参数与配置不会再各写一份。
#
# 覆盖手段：设 WAIT_FOR_DEPS（空格分隔的 host:port 列表）可以完全接管；
# 超时由 WAIT_FOR_DEPS_TIMEOUT 控制（秒，默认 60）。超时即 exit 1，并把
# 「是哪个依赖、等了多久、怎么查」明确打出来 —— 不静默失败。
set -eu

TIMEOUT="${WAIT_FOR_DEPS_TIMEOUT:-60}"

if [ "$#" -eq 0 ]; then
    echo "wait-for-deps: 用法: wait-for-deps.sh <cmd> [args...] —— 必须给出要执行的命令" >&2
    exit 2
fi

targets=""
if [ -n "${DATABASE_HOST:-}" ]; then
    targets="$targets ${DATABASE_HOST}:${DATABASE_PORT:-5432}"
fi
if [ -n "${REDIS_URL:-}" ]; then
    # redis://host:6379/0 → host:6379（去掉 scheme 与 path）
    targets="$targets $(printf '%s' "$REDIS_URL" | sed -E 's|^[A-Za-z]+://||; s|/.*$||')"
fi
# 显式覆盖优先（也给「等一个不在配置里的东西」留出口）
targets="${WAIT_FOR_DEPS:-$targets}"

if [ -z "$targets" ]; then
    echo "wait-for-deps: 没有可推导的依赖（DATABASE_HOST / REDIS_URL 都为空），直接启动" >&2
    exec "$@"
fi

for target in $targets; do
    host="${target%%:*}"
    port="${target##*:}"
    waited=0
    while ! nc -z "$host" "$port" 2>/dev/null; do
        if [ "$waited" -ge "$TIMEOUT" ]; then
            echo "wait-for-deps: 等 ${host}:${port} 超过 ${TIMEOUT}s 仍不可达，放弃启动。" >&2
            echo "wait-for-deps: 宿主机上的数据库/缓存起了吗？本机用 tools/local-infra.sh start 启停。" >&2
            exit 1
        fi
        sleep 1
        waited=$((waited + 1))
    done
    echo "wait-for-deps: ${host}:${port} 就绪（等待 ${waited}s）"
done

exec "$@"
