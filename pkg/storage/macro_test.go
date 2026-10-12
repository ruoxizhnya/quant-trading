package storage

// 宏观指标序列（P2-1，Migration 050 / MacroSeriesSchemaDDL）的测试。
//
// ─── 真库测试纪律（照 supply_chain_test.go，红线区） ────────────────────
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一个 source 标记
//     （itest-<uuid>），清理只 `DELETE ... WHERE source = $1`。
//     **绝不**无 WHERE 删表——本机 5432 的 quant_trading 是真库（AUD-62 教训）。
//  2. **库不在就红，不 skip**：真库用例连接失败即 t.Fatalf。
//  3. 时间戳取 2200 基准区，避开真实业务时间。

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 频率字面量（与 pkg/data 的 mcFreqMonthly/Daily 同值；storage 包不
// 依赖 data 包，测试内自持常量）。
const (
	mcFreqMonthly = "M"
	mcFreqDaily   = "D"
)

// mcCleanup 删 quant.macro_series 里本标记的行（幂等，清理失败只记不
// Fatalf——真库残留需要可见，但不该掩盖用例本身的结论）。
func mcCleanup(t *testing.T, store *PostgresStore, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := store.DB().Exec(ctx, "DELETE FROM quant.macro_series WHERE source = $1", source); err != nil {
		t.Errorf("清理 quant.macro_series 残留失败（source=%s，需手工检查）: %v", source, err)
	}
}

// mcCount 数 quant.macro_series 里本标记的行数。
func mcCount(t *testing.T, store *PostgresStore, source string) int {
	t.Helper()
	var n int
	require.NoError(t, store.DB().QueryRow(context.Background(),
		"SELECT count(*) FROM quant.macro_series WHERE source = $1", source).Scan(&n))
	return n
}

// ─── DDL 一致性（无库，破坏腿①/③） ──────────────────────────────────────

// TestMacroSchemaDDLSyncedWithMigrate 钉住「migrate() 实际执行的语句
// ≡ 包级 MacroSeriesSchemaDDL」：migrate() 数组里 Migration 050 追加的那
// 一段与包级变量逐条相等。本测试不需要真库。
//
// 段定位用 findMigrationSegment 按内容找（同 supply_chain_test.go，
// 不假设自己是最后一个 migration）。
//
// 破坏腿：改 postgres.go 的 MacroSeriesSchemaDDL 而不改 migrate() 的追加
// 内容（或反之）→ 本测试红。
func TestMacroSchemaDDLSyncedWithMigrate(t *testing.T) {
	t.Parallel()

	stmts := migrationStatements()
	seg, start := findMigrationSegment(t, stmts, MacroSeriesSchemaDDL)

	require.Len(t, MacroSeriesSchemaDDL, 3,
		"Migration 050 应为 3 条（schema + 1 表 + 1 索引）——"+
			"条数变了先确认是不是有意增删")
	for i := range MacroSeriesSchemaDDL {
		got := normalizeWS(seg[i])
		want := normalizeWS(MacroSeriesSchemaDDL[i])
		require.Equal(t, want, got,
			"migrate() 第 %d 条（Migration 050 段）与 MacroSeriesSchemaDDL 不一致："+
				"改任一处必须同步另一处", start+i+1)
	}
}

// TestMacroSchemaDDLSyncedWithDocsMirror 钉住包级变量与
// docs/migrations/050 镜像副本逐条相等——镜像漂移会让「文档里的 schema」与
// 「库里真建的 schema」悄悄分家。本测试不需要真库。
func TestMacroSchemaDDLSyncedWithDocsMirror(t *testing.T) {
	t.Parallel()

	mirror := sqlStatements(t, "../../docs/migrations/050_add_macro_series_table.sql")
	require.Len(t, mirror, len(MacroSeriesSchemaDDL),
		"镜像副本语句数与 MacroSeriesSchemaDDL 不一致（改一处不改另一处 → 漂移）")
	for i := range MacroSeriesSchemaDDL {
		require.Equal(t, normalizeWS(MacroSeriesSchemaDDL[i]), mirror[i],
			"第 %d 条 DDL 与镜像副本不一致", i+1)
	}
}

// TestMacroSchemaDDL_HasProvenanceColumns 是溯源约束护栏（破坏腿①）：
// macro_series 必须带 source TEXT NOT NULL + as_of TIMESTAMPTZ NOT NULL +
// 唯一约束。「无来源单源」（ODR-052 C4）的地基就是这两列——少了 NOT NULL，
// 一条无来源宏观数据能悄悄落库，而本层宣称的「带溯源」当场变成假话。
//
// 破坏腿①：删 source 的 NOT NULL → 本测试红。
func TestMacroSchemaDDL_HasProvenanceColumns(t *testing.T) {
	t.Parallel()

	var ddl string
	for _, stmt := range MacroSeriesSchemaDDL {
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS quant.macro_series") {
			ddl = stmt
		}
	}
	require.NotEmpty(t, ddl, "MacroSeriesSchemaDDL 应建 quant.macro_series 表")

	norm := normalizeWS(ddl)
	assert.Contains(t, norm, "source TEXT NOT NULL",
		"macro_series 缺 source TEXT NOT NULL（溯源列被削弱——破坏腿①）")
	assert.Contains(t, norm, "as_of TIMESTAMPTZ NOT NULL",
		"macro_series 缺 as_of TIMESTAMPTZ NOT NULL（观察时点被削弱）")
	assert.Contains(t, norm, "UNIQUE (indicator, freq, obs_date, source)",
		"macro_series 缺唯一约束 indicator+freq+obs_date+source（幂等键没了，重导会翻倍——破坏腿③的库内形态）")
	assert.Contains(t, norm, "indicator TEXT NOT NULL", "macro_series 缺 indicator 身份列")
	assert.Contains(t, norm, "freq TEXT NOT NULL", "macro_series 缺 freq 频率列")
	assert.Contains(t, norm, "obs_date DATE NOT NULL", "macro_series 缺 obs_date 观察日")
}

// ─── 真库：catalog 与 DDL 一致（验收④的 \d 等价物） ─────────────────────

// TestMacroSchema_RealDBCatalogMatchesDDL 在真库上核验 Migration 050：
// 表存在、source/as_of 列 NOT NULL、唯一约束按 DDL 声明落地、重放 DDL 是
// no-op（幂等）。这是「migrate 后 \d quant.macro_series 存在且与
// EnsureSchema 一致」的可执行形态——information_schema + pg_constraint 就是
// 无 psql 时的 \d。
func TestMacroSchema_RealDBCatalogMatchesDDL(t *testing.T) {
	store := scNewStore(t) // NewPostgresStore 已跑 migrate()（含 050）
	ctx := context.Background()
	db := store.DB()

	const table = "macro_series"
	fq := "quant." + table

	// 1) 表存在（information_schema.tables）。
	var exists bool
	require.NoError(t, db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables
		 WHERE table_schema = 'quant' AND table_name = $1)`, table).Scan(&exists))
	require.True(t, exists, "%s 不存在——Migration 050 没跑起来", fq)

	// 2) source / as_of / indicator / freq / obs_date 列存在且 NOT NULL。
	for _, col := range []string{"source", "as_of", "indicator", "freq", "obs_date", "value"} {
		var nullable string
		require.NoError(t, db.QueryRow(ctx,
			`SELECT is_nullable FROM information_schema.columns
			 WHERE table_schema = 'quant' AND table_name = $1 AND column_name = $2`,
			table, col).Scan(&nullable), "查列定义失败（%s.%s）", fq, col)
		require.Equal(t, "NO", nullable, "%s.%s 必须是 NOT NULL", fq, col)
	}

	// 3) 唯一约束按 DDL 声明落地（pg_constraint）。
	var conname string
	require.NoError(t, db.QueryRow(ctx,
		`SELECT conname FROM pg_constraint
		 WHERE conrelid = $1::regclass AND contype = 'u'`, fq).Scan(&conname))
	require.Equal(t, "uq_macro_series_key", conname,
		"%s 的唯一约束名与 DDL 不一致", fq)

	// 3b) 唯一约束的列集合就是 indicator+freq+obs_date+source（pg_attribute）。
	var cols []string
	rows, err := db.Query(ctx, `
		SELECT a.attname FROM pg_constraint c
		JOIN unnest(c.conkey) k(attnum) ON TRUE
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		WHERE c.conrelid = $1::regclass AND c.conname = 'uq_macro_series_key'
		ORDER BY array_position(c.conkey, a.attnum)`, fq)
	require.NoError(t, err)
	for rows.Next() {
		var col string
		require.NoError(t, rows.Scan(&col))
		cols = append(cols, col)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"indicator", "freq", "obs_date", "source"}, cols,
		"唯一键必须是 indicator+freq+obs_date+source（顺序即声明顺序）")

	// 4) 重放 MacroSeriesSchemaDDL 是 no-op（IF NOT EXISTS 幂等）。
	for i, ddl := range MacroSeriesSchemaDDL {
		_, err := db.Exec(ctx, ddl)
		require.NoErrorf(t, err, "重放第 %d 条 DDL 失败（幂等性被破坏）: %s", i+1, ddl)
	}
}

// ─── 真库：批量写入的事务语义 ───────────────────────────────────────────

// TestMacroStore_SaveBatchAllOrNothing 证明 batchExec 的「一个事务，全落
// 或全不落」：DB 执行期失败（obs_date 超 PG DATE 上限 5874897）必须零
// 落库——半批脏写比整批失败更难查。
func TestMacroStore_SaveBatchAllOrNothing(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { mcCleanup(t, store, source) })
	ctx := context.Background()

	rows := []*domain.MacroSeries{
		{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: 0.8, Unit: "%", Source: source, AsOf: scAsOf(1)},
		// 5875000 年 > PG DATE 上限 5874897：字符串参数穿过 pgx 编码检查，
		// PG 服务端执行时报越界——失败点在事务内部。
		{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: time.Date(5875000, 1, 1, 0, 0, 0, 0, time.UTC), Value: 1, Unit: "%", Source: source, AsOf: scAsOf(1)},
	}
	err := store.SaveMacroSeriesBatch(ctx, rows)
	require.Error(t, err, "DB 端越界必须整批报错（不许部分成功）")
	require.Contains(t, err.Error(), "macro_series", "错误应点名表名，便于定位")
	require.Equal(t, 0, mcCount(t, store, source),
		"事务回滚被破坏：合法行竟然落库了（半批脏写）")
}

// ─── 真库：Save/List roundtrip + 幂等（验收⑤） ─────────────────────────

// TestMacroStore_RoundtripFiltersAndIdempotency：Save 两遍行数不变（幂等，
// 验收⑤），List 按指标+频率+日期区间读回，空结果是空切片而非 error。
// 破坏腿③的 Red 腿指回这里：唯一约束没了 → 行数翻倍。
func TestMacroStore_RoundtripFiltersAndIdempotency(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { mcCleanup(t, store, source) })
	ctx := context.Background()

	rows := []*domain.MacroSeries{
		{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: 0.8, Unit: "%", Source: source, AsOf: scAsOf(1)},
		{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(1), Value: 0.5, Unit: "%", Source: source, AsOf: scAsOf(1)},
		{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(2), Value: 0.3, Unit: "%", Source: source, AsOf: scAsOf(1)},
		{Indicator: source + "-USDCNY_CLOSE", Freq: mcFreqDaily, ObsDate: scAsOf(0), Value: 6.95, Unit: "汇率", Source: source, AsOf: scAsOf(1)},
	}
	for i := 1; i <= 2; i++ {
		require.NoErrorf(t, store.SaveMacroSeriesBatch(ctx, rows), "第 %d 遍 macro Save 失败", i)
	}
	require.Equal(t, 4, mcCount(t, store, source),
		"macro 幂等：同唯一键导两遍行数必须不变（破坏腿③）")

	// 重导刷新取值（DO UPDATE，不是 DO NOTHING）：同键 value 修正。
	rows[0].Value = 0.9
	require.NoError(t, store.SaveMacroSeriesBatch(ctx, rows))
	got, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.InDelta(t, 0.9, got[2].Value, 1e-12,
		"同键重导应刷新取值（指标会被重述修正）；R4 降序后最旧一行在末位")
	require.True(t, got[0].ObsDate.After(got[1].ObsDate), "应按 obs_date 降序（R4：最新在前）")

	// 日期区间过滤（闭区间）。
	start, end := scAsOf(1), scAsOf(2)
	ranged, err := store.ListMacroSeries(ctx, MacroSeriesFilter{
		Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, StartDate: &start, EndDate: &end,
	})
	require.NoError(t, err)
	require.Len(t, ranged, 2, "日期区间闭区间应剩 2 条")

	// freq 过滤：日频行不被月频查询看到。
	daily, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: source + "-USDCNY_CLOSE", Freq: mcFreqDaily})
	require.NoError(t, err)
	require.Len(t, daily, 1)
	require.InDelta(t, 6.95, daily[0].Value, 1e-12)

	// indicator 必填。
	_, err = store.ListMacroSeries(ctx, MacroSeriesFilter{})
	require.Error(t, err, "indicator 必填（无过滤全表扫描被拒）")
	var appErr *apperrors.AppError
	require.True(t, errors.As(err, &appErr) && appErr.Code == apperrors.ErrCodeInvalidInput,
		"缺 indicator 应报 INVALID_INPUT，实际: %v", err)

	// 空结果是合法答案。
	empty, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: "NO_SUCH_INDICATOR"})
	require.NoError(t, err)
	require.Empty(t, empty, "空结果应是空切片而非 error")
}

// TestMacroStore_ListLimitAndDescOrder 钉住 R4：查询按 obs_date 降序
// （最新在前）、受 Limit 限制（截断只丢最旧的行）；未显式传 Limit 时
// 走默认值。破坏腿：去掉 ORDER BY DESC 或 LIMIT → 本测试红。
func TestMacroStore_ListLimitAndDescOrder(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { mcCleanup(t, store, source) })
	ctx := context.Background()

	rows := make([]*domain.MacroSeries, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, &domain.MacroSeries{
			Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly,
			ObsDate: scAsOf(i), Value: float64(i), Unit: "%", Source: source, AsOf: scAsOf(9),
		})
	}
	require.NoError(t, store.SaveMacroSeriesBatch(ctx, rows))

	// limit=2：只拿最新的 2 行（scAsOf(4) / scAsOf(3)），降序。
	got, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: source + "-CPI_YOY", Limit: 2})
	require.NoError(t, err)
	require.Len(t, got, 2, "limit=2 应恰好返回 2 行")
	require.Equal(t, scAsOf(4).Format("2006-01-02"), got[0].ObsDate.Format("2006-01-02"),
		"降序：第一行是最新的（截断只丢最旧的行）")
	require.Equal(t, scAsOf(3).Format("2006-01-02"), got[1].ObsDate.Format("2006-01-02"))

	// 未显式传 Limit：默认路径不截断 5 行小集合，但同样降序。
	all, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: source + "-CPI_YOY"})
	require.NoError(t, err)
	require.Len(t, all, 5, "默认 limit（1000）不截断 5 行小集合")
	require.Equal(t, scAsOf(4).Format("2006-01-02"), all[0].ObsDate.Format("2006-01-02"),
		"默认路径也是降序")
	require.Equal(t, scAsOf(0).Format("2006-01-02"), all[4].ObsDate.Format("2006-01-02"),
		"降序：最旧一行在末位")
}

// TestMacroStore_ListRejectsInvalidLimit：limit 越界 fail-loud
// （INVALID_INPUT）；0 是「未指定 → 默认」，不算错。校验先于查询
// （不碰库也返回 400 语义）。
func TestMacroStore_ListRejectsInvalidLimit(t *testing.T) {
	store := scNewStore(t)
	ctx := context.Background()

	for _, limit := range []int{-1, MacroSeriesMaxLimit + 1} {
		_, err := store.ListMacroSeries(ctx, MacroSeriesFilter{Indicator: "CPI_YOY", Limit: limit})
		require.Errorf(t, err, "limit=%d 应被拒", limit)
		var appErr *apperrors.AppError
		require.Truef(t, errors.As(err, &appErr) && appErr.Code == apperrors.ErrCodeInvalidInput,
			"limit=%d 拒绝原因应是 INVALID_INPUT，实际: %v", limit, err)
	}
}

// TestMacroStore_RejectsInvalidRows fail-loud：空身份列 / 零 as_of /
// 非有限数值一律被拒在库外，且不留下痕迹。
func TestMacroStore_RejectsInvalidRows(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { mcCleanup(t, store, source) })
	ctx := context.Background()

	cases := []struct {
		name string
		run  func() error
	}{
		{"缺 indicator", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: 1, Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"缺 freq", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", ObsDate: scAsOf(0), Value: 1, Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"缺 source（溯源列）", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: 1, AsOf: scAsOf(1)},
			})
		}},
		{"零 obs_date", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, Value: 1, Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"零 as_of", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: 1, Source: source},
			})
		}},
		{"NaN value", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: math.NaN(), Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"+Inf value", func() error {
			return store.SaveMacroSeriesBatch(ctx, []*domain.MacroSeries{
				{Indicator: source + "-CPI_YOY", Freq: mcFreqMonthly, ObsDate: scAsOf(0), Value: math.Inf(1), Source: source, AsOf: scAsOf(1)},
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			require.Error(t, err, "非法行必须被拒（fail-loud）")
			var appErr *apperrors.AppError
			require.True(t, errors.As(err, &appErr) && appErr.Code == apperrors.ErrCodeInvalidInput,
				"拒绝原因应是 INVALID_INPUT，实际: %v", err)
		})
	}

	// 全部被拒 ⇒ 一行都没落（无痕迹）。
	require.Equal(t, 0, mcCount(t, store, source))
}
