package storage

// 产业链静态数据层（P2-2，Migration 049 / SupplyChainSchemaDDL）的测试。
//
// ─── 真库测试纪律（照 signal_store_test.go / state_store_test.go，红线区） ─
//  1. **自己的数据自己造、自己验、自己删**：每个用例独享一个 source 标记
//     （itest-<uuid>），清理只 `DELETE ... WHERE source = $1`。
//     **绝不**无 WHERE 删表——本机 5432 的 quant_trading 是真库（AUD-62 教训）。
//  2. **库不在就红，不 skip**：真库用例连接失败即 t.Fatalf（与 testStore() 的
//     skip 惯例**故意不同**——本族用例自灌自证，红必须指向「库没起」这个真实
//     原因；起库后必须全绿）。
//  3. 时间戳取 2200 基准区，避开真实业务时间（同 signal store 先例）。

import (
	"context"
	"errors"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const scTestDSN = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"

// scNewStore 建真库 store（含 migrate()——Migration 049 随之执行）。
// 库不在就红不 skip：Fatalf 信息点名「先起 PG」。
func scNewStore(t *testing.T) *PostgresStore {
	t.Helper()
	store, err := NewPostgresStore(context.Background(), scTestDSN)
	if err != nil {
		t.Fatalf("连接测试库失败（DSN=%s；库不在就红不 skip——先起 PG 再跑）: %v", scTestDSN, err)
	}
	t.Cleanup(store.Close)
	return store
}

// scSource 生成本用例独享的 source 标记（清理只删这个标记的行）。
func scSource() string { return "itest-" + uuid.NewString() }

// scCleanup 删四张表里本标记的行（幂等，清理失败只记不 Fatalf——真库残留
// 需要可见，但不该掩盖用例本身的结论）。
func scCleanup(t *testing.T, store *PostgresStore, source string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, table := range []string{
		"quant.sw_industry_tree",
		"quant.supply_chain_edge",
		"quant.industry_vitality",
		"quant.etf_constituent",
	} {
		if _, err := store.DB().Exec(ctx, "DELETE FROM "+table+" WHERE source = $1", source); err != nil {
			t.Errorf("清理 %s 残留失败（source=%s，需手工检查）: %v", table, source, err)
		}
	}
}

// scCount 数一张表里本标记的行数。
func scCount(t *testing.T, store *PostgresStore, table, source string) int {
	t.Helper()
	var n int
	require.NoError(t, store.DB().QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE source = $1", source).Scan(&n))
	return n
}

// scAsOf 2200 基准区观察时点（避开真实业务时间）。
func scAsOf(i int) time.Time {
	return time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i)
}

// ─── DDL 一致性（无库，破坏腿①/③） ──────────────────────────────────────

// normalizeWS 归一化空白：把任意连续空白压成单个空格——缩进/换行风格差异
// 不算漂移（与 TestSignalSchemaDDLSyncedWithMigrate 同款比较口径）。
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestSupplyChainSchemaDDLSyncedWithMigrate 钉住「migrate() 实际执行的语句
// ≡ 包级 SupplyChainSchemaDDL」：migrate() 数组里 Migration 049 追加的那一段
// 与包级变量逐条相等。防的是有人在 migrate() 里内联一份漂移的拷贝，而两处
// 都自称正确、谁都发现不了（K8 破坏腿③同款 hazard）。本测试不需要真库。
//
// 段定位用 findMigrationSegment 按内容找（P2-1 之前是「取数组尾部」的
// 写法——那个假设「自己是最后一个 migration」，任何后续 migration 追加
// 都会把它打红，P2-1 追加 Migration 050 时实测命中，故改为按内容定位）。
//
// 破坏腿：改 postgres.go 的 SupplyChainSchemaDDL 而不改 migrate() 的追加
// 内容（或反之）→ 本测试红。
func TestSupplyChainSchemaDDLSyncedWithMigrate(t *testing.T) {
	t.Parallel()

	stmts := migrationStatements()
	seg, start := findMigrationSegment(t, stmts, SupplyChainSchemaDDL)

	require.Len(t, SupplyChainSchemaDDL, 9,
		"Migration 049 应为 9 条（schema + 4 表 + 4 索引）——"+
			"条数变了先确认是不是有意增删")
	for i := range SupplyChainSchemaDDL {
		got := normalizeWS(seg[i])
		want := normalizeWS(SupplyChainSchemaDDL[i])
		require.Equal(t, want, got,
			"migrate() 第 %d 条（Migration 049 段）与 SupplyChainSchemaDDL 不一致："+
				"改任一处必须同步另一处", start+i+1)
	}
}

// findMigrationSegment 在 migrate() 语句数组里按**内容**定位一段 migration
// （归一化空白后逐条相等的连续子序列），返回该段与起始下标（0 基）。
// 按内容找而不是取尾部：migrationStatements() 是append 增长的，任何后续
// migration 都会让「尾部」假设失效（P2-1 追加 050 时的实测教训）。
func findMigrationSegment(t *testing.T, stmts []string, segment []string) ([]string, int) {
	t.Helper()
	if len(segment) == 0 {
		t.Fatal("segment 为空")
	}
	for start := 0; start+len(segment) <= len(stmts); start++ {
		ok := true
		for i := range segment {
			if normalizeWS(stmts[start+i]) != normalizeWS(segment[i]) {
				ok = false
				break
			}
		}
		if ok {
			return stmts[start : start+len(segment)], start
		}
	}
	t.Fatalf("migrate() 里找不到与包级 DDL 变量一致的段（%d 条）——"+
		"该 migration 没被追加进 migrationStatements()？", len(segment))
	return nil, -1
}

// sqlStatement 从镜像 .sql 文件抽出「归一化空白后的语句序列」：剥掉 -- 注释
// 行，按分号切条，丢空条。048 先例的同款格式（注释头 + 逐条语句）。
func sqlStatements(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "读不到镜像副本 %s", path)

	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		kept = append(kept, line)
	}
	joined := strings.Join(kept, "\n")

	var out []string
	for _, stmt := range strings.Split(joined, ";") {
		if n := normalizeWS(stmt); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// TestSupplyChainSchemaDDLSyncedWithDocsMirror 钉住包级变量与
// docs/migrations/049 镜像副本逐条相等——镜像漂移会让「文档里的 schema」与
// 「库里真建的 schema」悄悄分家，而后者才是唯一执行路径。本测试不需要真库。
func TestSupplyChainSchemaDDLSyncedWithDocsMirror(t *testing.T) {
	t.Parallel()

	mirror := sqlStatements(t, "../../docs/migrations/049_add_supply_chain_tables.sql")
	require.Len(t, mirror, len(SupplyChainSchemaDDL),
		"镜像副本语句数与 SupplyChainSchemaDDL 不一致（改一处不改另一处 → 漂移）")
	for i := range SupplyChainSchemaDDL {
		require.Equal(t, normalizeWS(SupplyChainSchemaDDL[i]), mirror[i],
			"第 %d 条 DDL 与镜像副本不一致", i+1)
	}
}

// TestSupplyChainSchemaDDL_HasProvenanceColumns 是溯源约束护栏（破坏腿①）：
// 四张表都必须带 source TEXT NOT NULL + as_of TIMESTAMPTZ NOT NULL + 唯一
// 约束。「无来源单源」（ODR-052 C4）的地基就是这两列——少了 NOT NULL，
// 一条无来源数据能悄悄落库，而静态层宣称的「带溯源」当场变成假话。
//
// 破坏腿①：删任一表 source 的 NOT NULL → 本测试红。
func TestSupplyChainSchemaDDL_HasProvenanceColumns(t *testing.T) {
	t.Parallel()

	// 表名 → 该表 DDL 在 SupplyChainSchemaDDL 里的下标（按 CREATE TABLE 定位）。
	tableDDL := map[string]string{}
	for _, stmt := range SupplyChainSchemaDDL {
		if m := regexp.MustCompile(`CREATE TABLE IF NOT EXISTS quant\.(\w+)`).FindStringSubmatch(stmt); m != nil {
			tableDDL[m[1]] = stmt
		}
	}
	want := []string{"sw_industry_tree", "supply_chain_edge", "industry_vitality", "etf_constituent"}
	require.Len(t, tableDDL, len(want), "SupplyChainSchemaDDL 应恰好建四张表")

	for _, table := range want {
		ddl := tableDDL[table]
		norm := normalizeWS(ddl)
		assert.Contains(t, norm, "source TEXT NOT NULL",
			"%s 缺 source TEXT NOT NULL（溯源列被削弱——破坏腿①）", table)
		assert.Contains(t, norm, "as_of TIMESTAMPTZ NOT NULL",
			"%s 缺 as_of TIMESTAMPTZ NOT NULL（观察时点被削弱）", table)
		assert.Contains(t, norm, "UNIQUE",
			"%s 缺唯一约束（幂等键没了，重导会翻倍——破坏腿③的库内形态）", table)
	}
}

// ─── 真库：catalog 与 DDL 一致（验收④的 \d 等价物） ─────────────────────

// TestSupplyChainSchema_RealDBCatalogMatchesDDL 在真库上核验 Migration 049：
// 四表存在、source/as_of 列 NOT NULL、唯一约束按 DDL 声明落地、重放 DDL 是
// no-op（幂等）。这是「migrate 后 \d quant.sw_industry_tree 存在且与
// EnsureSchema 一致」的可执行形态——information_schema + pg_constraint 就是
// 无 psql 时的 \d。
func TestSupplyChainSchema_RealDBCatalogMatchesDDL(t *testing.T) {
	store := scNewStore(t) // NewPostgresStore 已跑 migrate()（含 049）
	ctx := context.Background()
	db := store.DB()

	tables := []string{"sw_industry_tree", "supply_chain_edge", "industry_vitality", "etf_constituent"}
	uniqueConstraints := map[string]string{
		"sw_industry_tree":  "uq_sw_industry_tree_code",
		"supply_chain_edge": "uq_supply_chain_edge_triple",
		"industry_vitality": "uq_industry_vitality_key",
		"etf_constituent":   "uq_etf_constituent_key",
	}

	for _, table := range tables {
		fq := "quant." + table

		// 1) 表存在（information_schema.tables）。
		var exists bool
		require.NoError(t, db.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'quant' AND table_name = $1)`, table).Scan(&exists),
			"查 information_schema 失败（%s）", fq)
		require.True(t, exists, "%s 不存在——Migration 049 没跑起来", fq)

		// 2) source / as_of 列存在且 NOT NULL（information_schema.columns）。
		for _, col := range []string{"source", "as_of"} {
			var nullable string
			require.NoError(t, db.QueryRow(ctx,
				`SELECT is_nullable FROM information_schema.columns
				 WHERE table_schema = 'quant' AND table_name = $1 AND column_name = $2`,
				table, col).Scan(&nullable),
				"查列定义失败（%s.%s）", fq, col)
			require.Equal(t, "NO", nullable,
				"%s.%s 必须是 NOT NULL（溯源列被削弱）", fq, col)
		}

		// 3) 唯一约束按 DDL 声明落地（pg_constraint）。
		var conname string
		require.NoError(t, db.QueryRow(ctx,
			`SELECT conname FROM pg_constraint
			 WHERE conrelid = $1::regclass AND contype = 'u'`, fq).Scan(&conname),
			"查唯一约束失败（%s）", fq)
		require.Equal(t, uniqueConstraints[table], conname,
			"%s 的唯一约束名与 DDL 不一致", fq)
	}

	// 4) 重放 SupplyChainSchemaDDL 是 no-op（IF NOT EXISTS 幂等）——「与
	// EnsureSchema 一致」的库内形态：同样语句再跑一遍不报错、结构不变。
	for i, ddl := range SupplyChainSchemaDDL {
		_, err := db.Exec(ctx, ddl)
		require.NoErrorf(t, err, "重放第 %d 条 DDL 失败（幂等性被破坏）: %s", i+1, ddl)
	}
}

// ─── 真库：批量写入的事务语义 ───────────────────────────────────────────

// TestSupplyChainStore_SaveBatchAllOrNothing 证明 batchExec 的「一个事务，
// 全落或全不落」不是注释空话。两个子用例覆盖**两种失败时点**：
//
//	编码期失败：level 超 int4 范围——pgx 客户端按目标列类型（OID 23）做
//	  二进制范围检查，错误根本到不了 DB（实测错误文本含 "unable to encode"）；
//	DB 执行期失败：obs_date 超 PG DATE 上限（5874897）——日期走 dateOnly()
//	  字符串参数，pgx 按 text 编码放过，PG 服务端执行时报越界。这才是事务
//	  回滚语义的真正考场。
//
// 两个时点都必须 0 落库：半批脏写比整批失败更难查。
//
// 来历：P2-2 自审发现①——原实现声明了该语义却无测试证明（未证明的声明
// 等于没有，§8.5.3-2 同款 hazard）。破坏腿：把 batchExec 的 tx.SendBatch
// 换成 pool.SendBatch（绕开事务）→ DB 执行期用例红（合法行 autocommit
// 落库，count=1）；编码期用例仍绿（失败从不在 DB 发生，事务与否无关——
// 这正是两个子用例都要留的原因）。
func TestSupplyChainStore_SaveBatchAllOrNothing(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { scCleanup(t, store, source) })
	ctx := context.Background()

	t.Run("编码期失败同样零落库", func(t *testing.T) {
		rows := []*domain.SWIndustryNode{
			{Code: source + "-enc-ok", Level: 1, Name: "合法行", Source: source, AsOf: scAsOf(1)},
			{Code: source + "-enc-bad", Level: math.MaxInt64, Name: "int4 越界行", Source: source, AsOf: scAsOf(1)},
		}
		err := store.SaveIndustryTreeBatch(ctx, rows)
		require.Error(t, err, "int4 越界必须整批报错")
		require.Contains(t, err.Error(), "sw_industry_tree", "错误应点名表名，便于定位")
		require.Equal(t, 0, scCount(t, store, "quant.sw_industry_tree", source+"-enc%"),
			"编码期失败也必须零落库")
	})

	t.Run("DB执行期失败触发事务回滚", func(t *testing.T) {
		rows := []*domain.IndustryVitality{
			{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: scAsOf(0), Value: 13.42, Unit: "%", Source: source, AsOf: scAsOf(1)},
			// 5875000 年 > PG DATE 上限 5874897：字符串参数穿过 pgx 编码检查，
			// PG 服务端执行时报越界——失败点在事务内部。
			{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: time.Date(5875000, 1, 1, 0, 0, 0, 0, time.UTC), Value: 1, Unit: "%", Source: source, AsOf: scAsOf(1)},
		}
		err := store.SaveIndustryVitalityBatch(ctx, rows)
		require.Error(t, err, "DB 端越界必须整批报错（不许部分成功）")
		require.Contains(t, err.Error(), "industry_vitality", "错误应点名表名，便于定位")
		require.Equal(t, 0, scCount(t, store, "quant.industry_vitality", source),
			"事务回滚被破坏：合法行竟然落库了（半批脏写）")
	})
}

// ─── 真库：Save/List roundtrip + 幂等（验收⑤） ─────────────────────────

// TestSupplyChainStore_RoundtripFiltersAndIdempotency 一枪四表：Save 两遍
// 行数不变（幂等，验收⑤），List 按各表过滤条件读回，空结果是空切片而非
// error。破坏腿②/③的 Red 腿都指回这里：唯一约束没了 → 行数翻倍。
func TestSupplyChainStore_RoundtripFiltersAndIdempotency(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { scCleanup(t, store, source) })
	ctx := context.Background()

	// ── 行业树 ──
	tree := []*domain.SWIndustryNode{
		{Code: source + "-L1", ParentCode: "", Level: 1, Name: "测试一级", IsLeaf: false, Source: source, AsOf: scAsOf(1)},
		{Code: source + "-L2", ParentCode: source + "-L1", Level: 2, Name: "测试二级", IsLeaf: true, Source: source, AsOf: scAsOf(1)},
		{Code: source + "-L2B", ParentCode: source + "-L1", Level: 2, Name: "测试二级B", IsLeaf: true, Source: source, AsOf: scAsOf(1)},
	}
	for i := 1; i <= 2; i++ {
		require.NoErrorf(t, store.SaveIndustryTreeBatch(ctx, tree), "第 %d 遍行业树 Save 失败", i)
	}
	require.Equal(t, 3, scCount(t, store, "quant.sw_industry_tree", source),
		"行业树幂等：同批导两遍行数必须不变（破坏腿③）")

	all, err := store.ListIndustryTree(ctx, IndustryTreeFilter{})
	require.NoError(t, err)
	require.NotEmpty(t, all, "全量树不应为空（库里可能有别人的行）")

	l1 := 1
	roots, err := store.ListIndustryTree(ctx, IndustryTreeFilter{Level: &l1})
	require.NoError(t, err)
	// 别人的一级行业也可能在库里；只断言本用例的节点在集合里且层级正确。
	var sawRoot bool
	for _, n := range roots {
		if n.Code == source+"-L1" {
			sawRoot = true
			require.Equal(t, 1, n.Level)
			require.Empty(t, n.ParentCode)
		}
	}
	require.True(t, sawRoot, "level=1 过滤应含本用例的一级节点")

	children, err := store.ListIndustryTree(ctx, IndustryTreeFilter{ParentCode: &tree[0].Code})
	require.NoError(t, err)
	require.Len(t, children, 2, "parent_code 过滤应只返回两个子节点")
	require.True(t, children[0].IsLeaf, "叶子标记应原样读回")

	// ── 上下游映射 ──
	edges := []*domain.SupplyChainEdge{
		{Upstream: source + "-stock", Downstream: source + "-ind", Relation: domain.RelationIndustryMember, Source: source, AsOf: scAsOf(1)},
		{Upstream: source + "-stock", Downstream: source + "-ind2", Relation: domain.RelationIndustryMember, Source: source, AsOf: scAsOf(1)},
	}
	for i := 1; i <= 2; i++ {
		require.NoErrorf(t, store.SaveSupplyChainEdgeBatch(ctx, edges), "第 %d 遍映射 Save 失败", i)
	}
	require.Equal(t, 2, scCount(t, store, "quant.supply_chain_edge", source), "映射幂等：行数必须不变")

	byDown, err := store.ListSupplyChainEdges(ctx, SupplyChainEdgeFilter{Downstream: source + "-ind"})
	require.NoError(t, err)
	require.Len(t, byDown, 1)
	require.Equal(t, domain.RelationIndustryMember, byDown[0].Relation)
	require.InDelta(t, 0.0, byDown[0].Weight, 1e-12, "成分关系 weight 恒 0")

	_, err = store.ListSupplyChainEdges(ctx, SupplyChainEdgeFilter{})
	require.Error(t, err, "无过滤全表扫描必须被拒（图结构表不接受）")
	var appErr *apperrors.AppError
	require.True(t, errors.As(err, &appErr) && appErr.Code == apperrors.ErrCodeInvalidInput,
		"无过滤查询应报 INVALID_INPUT，实际: %v", err)

	// ── 行业景气 ──
	vitality := []*domain.IndustryVitality{
		{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: scAsOf(0), Value: 13.42, Unit: "%", Source: source, AsOf: scAsOf(1)},
		{IndustryCode: source + "-ind", Metric: "gross_margin", ObsDate: scAsOf(1), Value: 14.01, Unit: "%", Source: source, AsOf: scAsOf(1)},
		{IndustryCode: source + "-ind", Metric: "revenue_yoy", ObsDate: scAsOf(1), Value: -2.31, Unit: "%", Source: source, AsOf: scAsOf(1)},
	}
	for i := 1; i <= 2; i++ {
		require.NoErrorf(t, store.SaveIndustryVitalityBatch(ctx, vitality), "第 %d 遍景气 Save 失败", i)
	}
	require.Equal(t, 3, scCount(t, store, "quant.industry_vitality", source), "景气幂等：行数必须不变")

	vrows, err := store.ListIndustryVitality(ctx, IndustryVitalityFilter{IndustryCode: source + "-ind"})
	require.NoError(t, err)
	require.Len(t, vrows, 3)
	require.Equal(t, "gross_margin", vrows[0].Metric)
	require.True(t, vrows[0].ObsDate.Before(vrows[1].ObsDate), "应按 obs_date 排序")
	require.InDelta(t, 13.42, vrows[0].Value, 1e-12)

	start := scAsOf(1)
	vrange, err := store.ListIndustryVitality(ctx, IndustryVitalityFilter{
		IndustryCode: source + "-ind", Metric: "gross_margin", StartDate: &start, EndDate: &start,
	})
	require.NoError(t, err)
	require.Len(t, vrange, 1, "日期区间+指标过滤应只剩 1 条")
	require.InDelta(t, 14.01, vrange[0].Value, 1e-12)

	// 空结果是合法答案：查一个不存在的行业 → 空切片 + nil error。
	empty, err := store.ListIndustryVitality(ctx, IndustryVitalityFilter{IndustryCode: "NO_SUCH_INDUSTRY"})
	require.NoError(t, err)
	require.Empty(t, empty, "空结果应是空切片而非 error")

	// ── ETF 成分 ──
	etf := []*domain.ETFConstituent{
		{ETFCode: source + "-etf", Symbol: "600519.SH", Weight: 5.23, ObsDate: scAsOf(0), Source: source, AsOf: scAsOf(1)},
		{ETFCode: source + "-etf", Symbol: "000001.SZ", Weight: 4.11, ObsDate: scAsOf(0), Source: source, AsOf: scAsOf(1)},
		{ETFCode: source + "-etf", Symbol: "600519.SH", Weight: 5.90, ObsDate: scAsOf(1), Source: source, AsOf: scAsOf(1)},
	}
	for i := 1; i <= 2; i++ {
		require.NoErrorf(t, store.SaveETFConstituentBatch(ctx, etf), "第 %d 遍 ETF Save 失败", i)
	}
	require.Equal(t, 3, scCount(t, store, "quant.etf_constituent", source),
		"ETF 幂等：同 (etf,symbol,obs_date) 重导不插新行")

	latest, err := store.ListETFConstituents(ctx, ETFConstituentFilter{ETFCode: source + "-etf"})
	require.NoError(t, err)
	require.Len(t, latest, 1, "默认取最新报告期：旧报告期那行不可见")
	require.Equal(t, "600519.SH", latest[0].Symbol)
	require.InDelta(t, 5.90, latest[0].Weight, 1e-12, "权重应按 weight DESC 排首")

	old := scAsOf(0)
	prev, err := store.ListETFConstituents(ctx, ETFConstituentFilter{ETFCode: source + "-etf", ObsDate: &old})
	require.NoError(t, err)
	require.Len(t, prev, 2, "显式指定旧报告期应读到当时的两行（PIT）")

	etfEmpty, err := store.ListETFConstituents(ctx, ETFConstituentFilter{ETFCode: "NO_SUCH_ETF"})
	require.NoError(t, err)
	require.Empty(t, etfEmpty, "空结果应是空切片而非 error")
}

// TestSupplyChainStore_RejectsInvalidRows fail-loud：空身份列 / 零 as_of /
// 非有限数值一律被拒在库外，且不留下痕迹（同 PGSignalStore.Save 口径）。
func TestSupplyChainStore_RejectsInvalidRows(t *testing.T) {
	store := scNewStore(t)
	source := scSource()
	t.Cleanup(func() { scCleanup(t, store, source) })
	ctx := context.Background()

	cases := []struct {
		name string
		run  func() error
	}{
		{"行业树缺 code", func() error {
			return store.SaveIndustryTreeBatch(ctx, []*domain.SWIndustryNode{
				{Code: "", Name: "x", Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"行业树缺 source", func() error {
			return store.SaveIndustryTreeBatch(ctx, []*domain.SWIndustryNode{
				{Code: source + "-x", Name: "x", AsOf: scAsOf(1)},
			})
		}},
		{"行业树零 as_of", func() error {
			return store.SaveIndustryTreeBatch(ctx, []*domain.SWIndustryNode{
				{Code: source + "-x", Name: "x", Source: source},
			})
		}},
		{"映射缺 relation", func() error {
			return store.SaveSupplyChainEdgeBatch(ctx, []*domain.SupplyChainEdge{
				{Upstream: "a", Downstream: "b", Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"景气零 obs_date", func() error {
			return store.SaveIndustryVitalityBatch(ctx, []*domain.IndustryVitality{
				{IndustryCode: "i", Metric: "m", Value: 1, Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"景气 NaN value", func() error {
			return store.SaveIndustryVitalityBatch(ctx, []*domain.IndustryVitality{
				{IndustryCode: "i", Metric: "m", ObsDate: scAsOf(0), Value: math.NaN(), Source: source, AsOf: scAsOf(1)},
			})
		}},
		{"ETF 缺 symbol", func() error {
			return store.SaveETFConstituentBatch(ctx, []*domain.ETFConstituent{
				{ETFCode: "e", Weight: 1, ObsDate: scAsOf(0), Source: source, AsOf: scAsOf(1)},
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
	require.Equal(t, 0, scCount(t, store, "quant.sw_industry_tree", source))
	require.Equal(t, 0, scCount(t, store, "quant.supply_chain_edge", source))
	require.Equal(t, 0, scCount(t, store, "quant.industry_vitality", source))
	require.Equal(t, 0, scCount(t, store, "quant.etf_constituent", source))
}
