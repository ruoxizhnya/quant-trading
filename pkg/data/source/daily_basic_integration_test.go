package source

// D5 集成测试（验收⑤1 的收口）：**connector 注册 → 同步 → 因子计算**在
// **真库**上贯通。
//
// 与既有两层测试的分工：
//   - `tushare_daily_basic_adapter_test.go` 用内存替身验协议（声明/范围/汇总）；
//   - `pkg/storage/market_activity_test.go` 用裸 SQL 验读写（幂等 / NULL / PIT 上界）；
//   - 本文件把两端接起来：**真 adapter → 真 PostgresStore → 真 factor_cache**，
//     证明「挂新数据 → 可挖因子」不是靠替身拼出来的。
//
// 上游用 httptest stub（不碰外网、不吃配额），DB 用真库（CI 的 go job 有
// Postgres service container，本用例在 CI 会真实执行）。库不可达 → skip
// （与本包 / pkg/data 既有真库用例同款）。
//
// 数据纪律（storage 包红线，本文件同样遵守）：自己的数据自己造、自己验、
// 自己删。清理**按独享 symbol**（`ITESTD5.SH` 不可能与真实 ts_code 撞），
// 删完复核 0 行——绝不无 WHERE 删表。注意 source 列由数据层写成 "tushare"
// （那是「从哪个门进来的」，如实记录），故不能用 source 当测试标记。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/data"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// d5ITestSymbol 是集成用例独享的标的（market_activity / factor_cache 的
// 清理都按它定位，不误伤真数据）。
const d5ITestSymbol = "ITESTD5.SH"

// d5ITestDSN 取测试库 DSN。
//
// ⚠️ 必须走 storage.BuildDSN：`pkg/storage` 是**唯一**允许构造 PostgreSQL
// DSN 的地方（AUD-42/ODR-065 结构护栏，`TestNoHandRolledPostgresDSNAssembly
// OutsideStorage` 会把包外的手拼 DSN 判红——本文件初版就被它抓了一次）。
// 该构造器同时负责密码转义与空密码拒绝（AUD-39 的教训）。
func d5ITestDSN(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("QUANT_TEST_DSN"); v != "" {
		return v
	}
	port := 5432
	if p, err := strconv.Atoi(os.Getenv("DATABASE_PORT")); err == nil && p > 0 {
		port = p
	}
	dsn, err := storage.BuildDSN(storage.DatabaseConfig{
		URL:      os.Getenv("DATABASE_URL"),
		Host:     os.Getenv("DATABASE_HOST"),
		Port:     port,
		User:     envOrDefault("DATABASE_USER", "postgres"),
		Password: envOrDefault("DATABASE_PASSWORD", "postgres"),
		Name:     envOrDefault("DATABASE_NAME", "quant_trading"),
		SSLMode:  "disable",
	})
	require.NoError(t, err, "测试 DSN 构造失败（唯一入口 storage.BuildDSN）")
	return dsn
}

// envOrDefault 读环境变量，空则取默认值。
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// openD5Store 打开真库（migrate() 会带上 Migration 056 的 market_activity）。
func openD5Store(t *testing.T) *storage.PostgresStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := storage.NewPostgresStore(ctx, d5ITestDSN(t))
	if err != nil {
		t.Skipf("跳过 D5 集成用例：连不上真库（%v）。本机先起 PG（127.0.0.1:5432/quant_trading）再跑；CI 由 postgres service container 提供", err)
	}
	t.Cleanup(store.Close)
	return store
}

// d5EchoDateStub 起一个**回显请求 trade_date** 的 daily_basic stub：
// 每个交易日的请求返回该日一行（换手率由 turnoverFor 决定）。
// 回显是必须的——固定返回同一个日期会让 20 次请求全部落到同一唯一键上，
// 被幂等 upsert 折叠成 1 行，窗口就不成立了。
func d5EchoDateStub(t *testing.T, turnoverFor func(date string) float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			APIName string `json:"api_name"`
			Params  struct {
				TradeDate string `json:"trade_date"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.APIName != "daily_basic" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":-1,"msg":"stub: unknown api_name"}`))
			return
		}
		turnover := turnoverFor(req.Params.TradeDate)
		body := fmt.Sprintf(
			`{"code":0,"msg":"","data":{"fields":["ts_code","trade_date","turnover_rate","turnover_rate_f","volume_ratio"],"items":[[%q,%q,%v,%v,%v]]}}`,
			d5ITestSymbol, req.Params.TradeDate, turnover, turnover, 1.0)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// d5Cleanup 删本用例造的行并复核归零（失败只记不改判——残留需可见，但不掩盖用例结论）。
func d5Cleanup(t *testing.T, store *storage.PostgresStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.DB().Exec(ctx, "DELETE FROM market_activity WHERE symbol = $1", d5ITestSymbol); err != nil {
		t.Errorf("清理 market_activity 残留失败（symbol=%s，需手工检查）: %v", d5ITestSymbol, err)
	}
	if _, err := store.DB().Exec(ctx,
		"DELETE FROM factor_cache WHERE factor_name = $1 AND symbol = $2",
		string(domain.FactorMarketActivity), d5ITestSymbol); err != nil {
		t.Errorf("清理 factor_cache 残留失败（symbol=%s，需手工检查）: %v", d5ITestSymbol, err)
	}
	var leftover int
	if err := store.DB().QueryRow(ctx,
		"SELECT count(*) FROM market_activity WHERE symbol = $1", d5ITestSymbol).Scan(&leftover); err != nil {
		t.Errorf("复核清理结果失败: %v", err)
	} else if leftover != 0 {
		t.Errorf("清理后仍有 %d 行 market_activity 残留（symbol=%s）", leftover, d5ITestSymbol)
	}
}

// TestD5Integration_RegisterSyncComputeScores 是全链路集成用例：
//
//	注册（D2 契约）→ 同步（真 adapter 逐日拉取 → 真库 market_activity）
//	  → 因子计算（真 store 取数 → expression 引擎 → 真库 factor_cache）
//
// 数值面：20 个观测里最后一天（计算日）换手率 2.0、其余 1.0 ⇒ 计算日末点
// 的 20 点均值 = (19×1.0 + 2.0)/20 = 1.05，异常活跃度
// = 2.0/1.05 − 1 = 0.9047619047619048。
func TestD5Integration_RegisterSyncComputeScores(t *testing.T) {
	store := openD5Store(t)
	ctx := context.Background()

	// 计算日往前 20 天（含当日 20 个观测，满足 ts_mean(·,20) 的位置要求）。
	const observations = 20
	end := time.Date(2200, 9, 30, 0, 0, 0, 0, time.UTC) // 2200 基准区，避开真实业务时间
	start := end.AddDate(0, 0, -(observations - 1))

	t.Cleanup(func() { d5Cleanup(t, store) })

	// 最后一天（计算日）换手率 2.0，其余 1.0。
	lastDay := end.Format("20060102")
	srv := d5EchoDateStub(t, func(date string) float64 {
		if date == lastDay {
			return 2.0
		}
		return 1.0
	})
	client := data.NewTushareClient("test-token", srv.URL, 0, store, nil)
	adapter := NewTushareDailyBasicAdapter(client)

	// ① 注册：过 D2 契约的 fail-closed 校验并进 Registry。
	reg := NewRegistry()
	require.NoError(t, reg.Register(adapter), "adapter 必须注册成功（D2 契约）")
	require.Contains(t, reg.ListAdapters(), "tushare_daily_basic")

	// ② 同步：逐日拉取 → 落真库。
	resp, err := adapter.Fetch(ctx, FetchRequest{
		DataType:  DataTypeDailyBasic,
		StartDate: start,
		EndDate:   end,
	})
	require.NoError(t, err)
	require.Len(t, resp.Items, observations, "20 个自然日 → 20 行")

	var stored int
	require.NoError(t, store.DB().QueryRow(ctx,
		"SELECT count(*) FROM market_activity WHERE symbol = $1", d5ITestSymbol).Scan(&stored))
	require.Equal(t, observations, stored, "同步落库行数与 adapter 汇总一致")

	// ②' 幂等：重放同范围不增行（唯一键 upsert）。
	_, err = adapter.Fetch(ctx, FetchRequest{DataType: DataTypeDailyBasic, StartDate: start, EndDate: end})
	require.NoError(t, err)
	require.NoError(t, store.DB().QueryRow(ctx,
		"SELECT count(*) FROM market_activity WHERE symbol = $1", d5ITestSymbol).Scan(&stored))
	require.Equal(t, observations, stored, "重放同范围必须幂等（同键刷新不插新行）")

	// ③ 因子计算：真 store 取数 → 引擎 → 真 factor_cache。
	fc := data.NewFactorComputer(store)
	require.NoError(t, fc.ComputeFactorByName(ctx, domain.FactorMarketActivity, end))

	var raw, zscore, pct float64
	require.NoError(t, store.DB().QueryRow(ctx,
		`SELECT raw_value, z_score, percentile FROM factor_cache
		 WHERE factor_name = $1 AND symbol = $2 AND trade_date = $3`,
		string(domain.FactorMarketActivity), d5ITestSymbol, end).Scan(&raw, &zscore, &pct))

	const want = 2.0/1.05 - 1 // = 0.9047619047619048
	assert.InDelta(t, want, raw, 1e-9, "真库贯通后的原始分（手算口径）")
	assert.InDelta(t, 0.0, zscore, 1e-9, "单标的横截面无方差 → z-score 0")
	// 百分位口径是 0–100（SAS PERCENT_RANK：单标的 → (1−0.5)/1×100 = 50），
	// 不是 0–1——见 pkg/data/factor.go 的 PercentileRank 注释。
	assert.InDelta(t, 50.0, pct, 1e-9, "单标的百分位 = 50（0–100 标度）")

	// ④ PIT：计算日之后的观测不参与（真库侧上界执法）。造一行「未来」观测，
	// 重算同一日，分数必须逐位不变。
	future := 1000.0
	require.NoError(t, store.SaveMarketActivityBatch(ctx, []*domain.MarketActivity{{
		Symbol: d5ITestSymbol, TradeDate: end.AddDate(0, 0, 1),
		TurnoverRate: &future, Source: "itest-future", AsOf: time.Now().UTC(),
	}}))
	t.Cleanup(func() {
		ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = store.DB().Exec(ctx2, "DELETE FROM market_activity WHERE source = $1", "itest-future")
	})

	require.NoError(t, fc.ComputeFactorByName(ctx, domain.FactorMarketActivity, end))
	var rawAfter float64
	require.NoError(t, store.DB().QueryRow(ctx,
		`SELECT raw_value FROM factor_cache WHERE factor_name = $1 AND symbol = $2 AND trade_date = $3`,
		string(domain.FactorMarketActivity), d5ITestSymbol, end).Scan(&rawAfter))
	assert.InDelta(t, raw, rawAfter, 0.0, "晚于计算日的观测必须结构上不可见（真库侧 trade_date <= end）")
}
