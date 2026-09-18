#!/usr/bin/env bash
#
# 全量数据同步（P2-1 / P2-2 / P2-4 / P2-13 的共同前置）
#
# 库现在是空的：stocks / ohlcv_daily_qfq / stock_fundamentals / trading_calendar
# 全为 0 行。P2-4 刚修好的「按日在市过滤」、P2-13 的验证器链接真实回测取证，
# 都要先有数据才能兑现。
#
# 用法：
#   export TUSHARE_TOKEN=<你的 token>      # 或直接写进 .env（已被 git 忽略）
#   ./tools/sync_all.sh                    # 全量（默认 10 年）
#   YEARS=3 ./tools/sync_all.sh            # 只要最近 3 年，快很多
#
# 为什么整栈走 compose 而不是本地 go run：
#   data-service 的配置里数据库 host 是 "postgres"（compose 网络名），密码
#   也从 compose 环境继承。本地跑就得把 host 改成 localhost、把密码再抄一遍
#   —— 那等于把凭据散到第二个地方。跑在 compose 里，网络和密码都只有一份。
#
# 为什么必须走 HTTP：同步能力只以 HTTP 接口暴露在 cmd/data，没有 CLI 入口。

set -euo pipefail
cd "$(dirname "$0")/.."

YEARS="${YEARS:-10}"
PORT="${DATA_PORT:-8081}"

# 本机 docker compose 有两个写法（Rancher Desktop 只提供带横杠的那个）
if docker compose version >/dev/null 2>&1; then
	DC="docker compose"
elif docker-compose version >/dev/null 2>&1; then
	DC="docker-compose"
else
	echo "✗ 找不到 docker compose"
	exit 1
fi

if [ -z "${TUSHARE_TOKEN:-}" ] && ! grep -q 'TUSHARE_TOKEN=.\+' .env 2>/dev/null; then
	echo "✗ TUSHARE_TOKEN 未设置。"
	echo "  写进 .env（该文件已被 git 忽略）后重跑，或："
	echo "    export TUSHARE_TOKEN=... && ./tools/sync_all.sh"
	exit 1
fi

START_DATE="$(date -d "-${YEARS} years" +%Y%m%d 2>/dev/null || date -v-"${YEARS}"y +%Y%m%d)"
END_DATE="$(date +%Y%m%d)"

echo "→ 同步区间：${START_DATE} .. ${END_DATE}（YEARS=${YEARS}）"

# ── 1. 起依赖 + data-service ───────────────────────────────────────────
# Redis 是 data-service 的强依赖（连不上直接 Fatal），所以一并起。
echo "→ 启动 postgres / redis / data-service"
TUSHARE_TOKEN="${TUSHARE_TOKEN:-}" ${DC} up -d postgres redis data-service

echo "→ 等 data-service 就绪 (:${PORT})"
for _ in $(seq 1 90); do
	if curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
		break
	fi
	sleep 1
done
if ! curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
	echo "✗ data-service 没就绪，日志尾部："
	${DC} logs --tail=25 data-service
	exit 1
fi

post() {
	local path="$1" body="$2" label="$3"
	echo "→ ${label}"
	curl -sS -X POST "http://localhost:${PORT}${path}" \
		-H 'Content-Type: application/json' \
		-d "${body}" | head -c 400
	echo
}

# ── 2. 股票列表：ALL = 在市 + 退市 + 暂停上市 ──────────────────────────
# 只同步 "L"（在市）就是幸存者偏差的物理成因 —— 回测 2020 年时，2021 年
# 退市的票根本不在库里，它最惨的那段行情永远不会出现在任何回测结果中。
post /sync/stocks '{"list_status":"ALL"}' "股票列表（含退市，P2-4）"

# ── 3. 交易日历 ────────────────────────────────────────────────────────
post /sync/calendar \
	"{\"exchange\":\"both\",\"start_date\":\"${START_DATE}\",\"end_date\":\"${END_DATE}\"}" \
	"交易日历"

# ── 4. 全量行情（异步 job，量大）──────────────────────────────────────
post /sync/ohlcv/all \
	"{\"start_date\":\"${START_DATE}\",\"end_date\":\"${END_DATE}\",\"batch_size\":10}" \
	"全量行情（异步 job）"

cat <<EOF

→ 行情是异步 job，查进度：
    curl -s http://localhost:${PORT}/api/sync/jobs | head -c 500
    ${DC} logs -f data-service

→ 跑完后用这条确认库里真有数据：
    ${DC} exec -T postgres psql -U postgres -d quant_trading -c \\
      "select (select count(*) from stocks) stocks,
              (select count(*) from stocks where delist_date is not null) delisted,
              (select count(*) from ohlcv_daily_qfq) ohlcv,
              (select count(*) from trading_calendar) calendar"
EOF
