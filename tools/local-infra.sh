#!/bin/sh
# 本机原生基础设施启停（PostgreSQL + Redis）—— 「数据库跑宿主机、服务跑容器」
# 形态的统一入口。
#
# 用法: tools/local-infra.sh {start|stop|status}
#
# 为什么是原生而不是容器：见 docker-compose.yml 文件头。一句话是 Docker Desktop
# 在本机不稳定（沙箱拦 wsl.exe，且起来后还会自发退出），不该把最该稳的一层挂在
# 最不稳的一层上。
#
# ⚠️ **在 Agent 沙箱里跑 `start`，进程会随这次工具调用结束被一起清理**（沙箱按
#    进程组回收整棵进程树）。那种情况下要改用「后台任务」姿势直接 exec 这两个
#    二进制，不要指望本脚本的 `nohup ... &`。**在你自己开的终端里正常** ——
#    Windows 不会因为父 shell 退出而杀掉子进程。
#
# ⚠️ **AUD-13 的「只绑回环」断言现在住在这里**（原先在
#    tools/check_deploy_consistency.py 里守 compose 的 ports 映射）。服务移出
#    compose 后那个检查对象不存在了，绑定的真身变成了原生进程 —— 而绑定的真身
#    在仓外，静态脚本管不到，所以放到这个能读真实环境的脚本里。
#    这里断言的是 **netstat 的监听 socket**，不是配置文件的值：配置文件可以被
#    命令行参数覆盖（本脚本就是靠 --bind / -c 覆盖的）。
set -eu

PG_ROOT_WIN="${PG_ROOT_WIN:-C:/Users/ruoxi/.workbuddy/binaries/postgres}"
REDIS_ROOT_WIN="${REDIS_ROOT_WIN:-C:/Users/ruoxi/.workbuddy/binaries/redis-7.4.11}"
LOG_DIR_WIN="${LOG_DIR_WIN:-C:/Users/ruoxi/.workbuddy/binaries/logs}"
PG_PORT="${PG_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"

PG_BIN="$PG_ROOT_WIN/pgsql/bin"
PG_DATA="$PG_ROOT_WIN/data"
REDIS_BIN="$REDIS_ROOT_WIN"

usage() {
    echo "用法: $0 {start|stop|status}" >&2
    exit 2
}

# ── AUD-13：数据库/缓存只能监听回环 ────────────────────────────────────
# 返回 0 = 全部合规；1 = 有非回环监听；2 = 有进程没在跑
check_loopback() {
    rc=0
    found=0
    for pair in "PostgreSQL:$PG_PORT" "Redis:$REDIS_PORT"; do
        name="${pair%%:*}"
        port="${pair##*:}"
        lines=$(netstat -ano 2>/dev/null | grep LISTENING | grep -E ":${port}[[:space:]]" || true)
        if [ -z "$lines" ]; then
            echo "  - ${name} :${port} 未监听"
            rc=2
            continue
        fi
        found=1
        # netstat 的列: Proto  LocalAddress  ForeignAddress  State  PID
        bad=$(printf '%s\n' "$lines" | awk '{print $2}' \
              | grep -vE '^(127\.0\.0\.1|\[::1\]):' || true)
        addrs=$(printf '%s\n' "$lines" | awk '{print $2}' | tr '\n' ' ')
        if [ -n "$bad" ]; then
            echo "  ✗ ${name} 监听在非回环地址: ${bad} —— 已暴露到局域网（AUD-13）"
            rc=1
        else
            echo "  ✓ ${name} 只绑回环: ${addrs}"
        fi
    done
    if [ "$found" -eq 0 ]; then
        return 2
    fi
    return "$rc"
}

status() {
    echo "原生基础设施状态"
    echo "  PG 数据目录 : $PG_DATA"
    echo "  Redis 目录  : $REDIS_BIN"
    echo
    check_loopback || true
}

start() {
    mkdir -p "$LOG_DIR_WIN"

    echo "启动 PostgreSQL（$PG_DATA, 绑 127.0.0.1:$PG_PORT）..."
    # -c listen_addresses=127.0.0.1 是**显式**的：不靠配置文件里的默认值，
    # 免得哪天 postgresql.conf 被改成 '*' 而没人发现（AUD-13）。
    nohup "$PG_BIN/postgres.exe" -D "$PG_DATA" \
        -c "listen_addresses=127.0.0.1" -p "$PG_PORT" \
        > "$LOG_DIR_WIN/postgres.log" 2>&1 &
    echo "  pid=$!"

    echo "启动 Redis（$REDIS_BIN, 绑 127.0.0.1:$REDIS_PORT）..."
    # ⚠️ Redis 是 MSYS2 构建，**不认 Git Bash 的 /c/... 路径**（会当成 msys 根）,
    #    也不认 `C:/...`（会被当成相对 cwd 的路径拼上去）。唯一稳的姿势是
    #    cd 进去 + 用相对路径，别改成绝对路径。
    ( cd "$REDIS_BIN" && nohup ./redis-server.exe redis.conf \
        --bind 127.0.0.1 --port "$REDIS_PORT" --dir ./data --appendonly yes \
        > "$LOG_DIR_WIN/redis.log" 2>&1 & echo "  pid=$!" )

    echo
    echo "等待就绪（最多 30s）..."
    waited=0
    while [ "$waited" -lt 30 ]; do
        pg_ok=0; redis_ok=0
        "$PG_BIN/pg_isready.exe" -h 127.0.0.1 -p "$PG_PORT" >/dev/null 2>&1 && pg_ok=1
        "$REDIS_BIN/redis-cli.exe" -h 127.0.0.1 -p "$REDIS_PORT" ping 2>/dev/null \
            | grep -q PONG && redis_ok=1
        if [ "$pg_ok" -eq 1 ] && [ "$redis_ok" -eq 1 ]; then
            echo "  两个都就绪（${waited}s）"
            echo
            status
            return 0
        fi
        sleep 1
        waited=$((waited + 1))
    done

    echo "  超时：PostgreSQL 就绪=$pg_ok  Redis 就绪=$redis_ok" >&2
    echo "  看日志：$LOG_DIR_WIN/postgres.log  $LOG_DIR_WIN/redis.log" >&2
    return 1
}

stop() {
    echo "停止 PostgreSQL..."
    "$PG_BIN/pg_ctl.exe" -D "$PG_DATA" -m fast stop 2>&1 || echo "  （可能本来就没在跑）"
    echo "停止 Redis..."
    "$REDIS_BIN/redis-cli.exe" -h 127.0.0.1 -p "$REDIS_PORT" shutdown nosave 2>&1 \
        || echo "  （可能本来就没在跑）"
    echo
    status
}

case "${1:-}" in
    start)  start ;;
    stop)   stop ;;
    status) status ;;
    *)      usage ;;
esac
