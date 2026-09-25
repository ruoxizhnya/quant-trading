#!/bin/sh
# 整栈启停 + **健康校验**（原生基础设施 + 容器服务）。
#
# 用法: tools/local-stack.sh {start|stop|status}
#
# 形态（2026-09-25 定案）：数据库/缓存跑宿主机原生，应用服务跑容器。
# 本脚本是这两半的**唯一入口**，负责顺序、就绪等待与体检。
#
# ── 为什么必须「起来之后再看一眼」──────────────────────────────────────
#
# `docker-compose up -d` 返回 0 **只代表容器被创建了**，不代表服务可用：
#   - 应用连不上 PG/Redis 是 logger.Fatal()（无重试，cmd/data/setup.go:137/:146），
#     容器会立刻退出；
#   - 健康检查有 start_period，up 刚返回时状态还是 "starting"；
#   - Docker Desktop 在本机**会自发退出**（2026-09-25 实测：10:24 还在跑，
#     10:27 命名管道就没了），"起来了"不能当稳定前提。
# 所以本脚本把「HTTP 探针全通」当作唯一的成功判据，而不是 up 的退出码。
#
# ⚠️ **AUD-13 的运行时那一半住在这里**：analysis 以 open-access 运行，
#    而容器必须绑 0.0.0.0 —— 「不可从其他主机到达」这条不变量因此完全由
#    **发布层**承担。静态那一半在 tools/check_deploy_consistency.py 的检查 3c
#    （读 compose 文件）；这里读**真实 netstat**，断言四个端口只监听回环。
#    两者缺一不可：静态管不到 override 文件与运行时改动，运行时管不到未来
#    还没起的那次提交。
set -eu

ROOT_WIN="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_WIN"

# 容器服务 → 宿主端口（与 docker-compose.yml 的 127.0.0.1: 映射一一对应）
SERVICES="data-service:8081 strategy-service:8082 analysis-service:8085 web:8080"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-180}"

COMPOSE="docker-compose"
if ! command -v docker-compose >/dev/null 2>&1; then
    COMPOSE="docker compose"
fi

usage() {
    echo "用法: $0 {start|stop|status}" >&2
    exit 2
}

# ── Docker 引擎是否真的活着 ───────────────────────────────────────────
# 这不是多余的：Docker Desktop 会自发退出，此后所有 docker 命令都报
# "open //./pipe/dockerDesktopLinuxEngine: The system cannot find the file
# specified"，读起来像路径问题而不是「引擎没了」。
daemon_ok() {
    docker info --format '{{.ServerVersion}}' >/dev/null 2>&1
}

require_daemon() {
    if daemon_ok; then
        return 0
    fi
    echo "✗ Docker 引擎不可达 —— 本脚本只能停在这里。" >&2
    echo "  这是**沙箱层**的限制（启动 Docker Desktop 会拉起 wsl.exe，而它在" >&2
    echo "  程序黑名单里，报错明写「不可批准、不可绕过」），脚本无法自救。" >&2
    echo "  请你自己双击启动 Docker Desktop，等托盘图标变绿后重跑本脚本。" >&2
    return 1
}

# ── 单个端口的 HTTP 探针 ──────────────────────────────────────────────
http_ok() {
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$1$2" 2>/dev/null || true)
    [ "$code" = "200" ]
}

# ── 容器健康状态（compose 的 healthcheck 结论）────────────────────────
container_health() {
    docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$1" 2>/dev/null || echo missing
}

# ── AUD-13 运行时断言：四个端口只许监听回环 ───────────────────────────
check_host_loopback() {
    rc=0
    for pair in $SERVICES; do
        port="${pair##*:}"
        lines=$(netstat -ano 2>/dev/null | grep LISTENING | grep -E ":${port}[[:space:]]" || true)
        if [ -z "$lines" ]; then
            echo "  - :${port} 未监听（服务可能没起来）"
            rc=2
            continue
        fi
        # netstat 的列: Proto  LocalAddress  ForeignAddress  State  PID
        bad=$(printf '%s\n' "$lines" | awk '{print $2}' \
              | grep -vE '^(127\.0\.0\.1|\[::1\]):' || true)
        addrs=$(printf '%s\n' "$lines" | awk '{print $2}' | tr '\n' ' ')
        if [ -n "$bad" ]; then
            echo "  ✗ :${port} 监听在非回环地址: ${bad}"
            echo "    → AUD-13：本栈的所有端口都只许绑回环。analysis 以 open-access"
            echo "      运行（web 的 /api 还反代到它），暴露出去等于把下单接口开给局域网。"
            echo "      检查 docker-compose.yml 的 ports 是否都有 127.0.0.1: 前缀。"
            rc=1
        else
            echo "  ✓ :${port} 只绑回环: ${addrs}"
        fi
    done
    return "$rc"
}

stop() {
    require_daemon || { echo "（无引擎，跳过容器部分）"; return 0; }
    echo "停止容器服务（原生 PG/Redis 不动 —— 停它们用 tools/local-infra.sh stop）..."
    $COMPOSE down --remove-orphans
}

start() {
    require_daemon || return 1

    echo "① 原生基础设施"
    ./tools/local-infra.sh start || {
        echo "✗ 原生 PG/Redis 没起来，容器连不上库会立刻 Fatal 退出，先修这一层。" >&2
        return 1
    }

    echo
    echo "② 启动容器"
    $COMPOSE up -d --remove-orphans

    echo
    echo "③ 等健康（最多 ${HEALTH_TIMEOUT}s；判据是 HTTP 探针，不是 up 的退出码）"
    waited=0
    while [ "$waited" -lt "$HEALTH_TIMEOUT" ]; do
        all_ok=1
        detail=""
        for pair in $SERVICES; do
            svc="${pair%%:*}"
            port="${pair##*:}"
            # compose 给容器起的名字是 <project>-<svc>-1
            cname="$(docker ps --filter "name=${svc}" --format '{{.Names}}' | head -1)"
            h="missing"
            [ -n "$cname" ] && h="$(container_health "$cname")"
            if http_ok "$port" /health || http_ok "$port" /; then
                detail="$detail ${svc}=up"
            else
                all_ok=0
                detail="$detail ${svc}=${h}"
            fi
        done
        if [ "$all_ok" -eq 1 ]; then
            echo "  全部就绪（${waited}s）:${detail}"
            echo
            echo "④ 宿主机端口体检（AUD-13 运行时断言）"
            check_host_loopback || {
                echo "✗ 有端口暴露到非回环 —— 服务已起但不合规，请立刻处理。" >&2
                return 1
            }
            echo
            echo "✓ 整栈可用（open-access，仅本机可访问）"
            return 0
        fi
        sleep 2
        waited=$((waited + 2))
    done

    echo "✗ ${HEALTH_TIMEOUT}s 内未全部就绪:${detail}" >&2
    echo "  容器状态：" >&2
    docker ps --format '  {{.Names}}\t{{.Status}}' >&2 || true
    echo "  日志：$COMPOSE logs --tail=50 <服务名>" >&2
    return 1
}

status() {
    echo "Docker 引擎"
    if daemon_ok; then
        echo "  ✓ server=$(docker info --format '{{.ServerVersion}}' 2>/dev/null)"
    else
        echo "  ✗ 不可达（Docker Desktop 没起来 / 已自发退出）"
    fi

    echo
    echo "容器服务"
    if daemon_ok; then
        for pair in $SERVICES; do
            svc="${pair%%:*}"
            port="${pair##*:}"
            cname="$(docker ps --filter "name=${svc}" --format '{{.Names}}' | head -1)"
            if [ -z "$cname" ]; then
                echo "  - ${svc} 未运行"
                continue
            fi
            h="$(container_health "$cname")"
            http="down"
            { http_ok "$port" /health || http_ok "$port" /; } && http="200"
            echo "  ${svc} :${port}  容器=${h}  HTTP=${http}"
        done
    fi

    echo
    echo "宿主机端口体检（AUD-13 运行时断言）"
    check_host_loopback || true

    echo
    echo "原生基础设施"
    ./tools/local-infra.sh status || true
}

case "${1:-}" in
    start)  start ;;
    stop)   stop ;;
    status) status ;;
    *)      usage ;;
esac
