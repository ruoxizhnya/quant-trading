package storage

// Migration 054（成交额 千元 → CNY）的迁移体测试。
//
// 为什么值得单独一个文件：这条 DML 一旦跑错，坏的是 392.6 万行历史成交额，
// 而下游（成交额类因子、表达式 `turnover` 字段、展示）不会报错，只会静默地
// 全体偏 1000 倍 —— 与 DATA-04 修的 volume 同一个病，只是没有整数校验替它喊。
// 所以这里把两道守卫各自钉成可执行的场景，而不是只在注释里讲道理。
//
// 隔离方式（真库一行不动）：会话 `SET search_path = pg_temp`，DO 块里的
// ohlcv_daily_qfq / data_migrations / bak 表全部只可能落在临时 schema；
// public 里的真表在本会话**不可见**，写错名字只会报错，不会改到真数据。
// 执行前再断言一次「非限定名读到的行数 = 本用例种子行数」，双保险。
//
// 全程只用**同一条连接**（不是池）：pg_temp 是会话级的，换连接就换到空会话上，
// 临时表与哨兵都会“神秘消失”，测出来的东西不算数。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// 与 pkg/storage/postgres.go 内联 DDL 里的哨兵名同一个字符串。
	turnoverMigrationName = "054_ohlcv_turnover_thousand_cny_to_cny"

	// 本机 PG。与同包其它库侧测试同一个连接串，但**不走** testStore /
	// NewPostgresStore —— 那两个会连带跑 migrate()，把本迁移真的作用到共享
	// 开发库的 ohlcv_daily_qfq 上。本测试只要一条裸连接。
	turnoverTestDSN = "postgres://postgres:postgres@localhost:5432/quant_trading?sslmode=disable"
)

// turnoverOHLCVRow 是种进临时 ohlcv_daily_qfq 的一行（只带本迁移看得见的列）。
type turnoverOHLCVRow struct {
	symbol   string
	close_   float64
	volume   float64
	turnover float64
}

// turnoverMigrationStmt 从内联 DDL 数组里按**内容**取出 054 那条 DO 块。
// 按内容找而不是取尾部：migrationStatements() 靠 append 增长，任何后续
// migration 都会让「尾部」假设失效（052 / P2-1 的实测教训）。
func turnoverMigrationStmt(t *testing.T) string {
	t.Helper()
	for _, stmt := range migrationStatements() {
		if strings.Contains(stmt, turnoverMigrationName) {
			return stmt
		}
	}
	t.Fatalf("内联 DDL 里找不到 %s —— 该迁移没被追加进 migrationStatements()？", turnoverMigrationName)
	return ""
}

// TestMigration054_StatementShape 静态形状检查：不连库也能钉住四件事 ——
// ① 有哨兵准入、② 整表 ×1000 确实发生、③ 备份在换算之前、④ 哨兵写在 IF 之外
// （跳过换算的那次启动也留痕）。任何一条被后来的编辑弄丢，本测试立刻红。
func TestMigration054_StatementShape(t *testing.T) {
	stmt := turnoverMigrationStmt(t)

	for _, want := range []string{
		"NOT EXISTS (SELECT 1 FROM data_migrations WHERE name = '" + turnoverMigrationName + "')",
		"UPDATE ohlcv_daily_qfq SET turnover = turnover * 1000",
		"CREATE TABLE IF NOT EXISTS ohlcv_daily_qfq_bak_054 AS",
		"INSERT INTO data_migrations (name) VALUES ('" + turnoverMigrationName + "')",
	} {
		if !strings.Contains(stmt, want) {
			t.Errorf("054 迁移体里缺 %q", want)
		}
	}
	// 哨兵必须落在 IF **之外**：内容守卫不放行时（清库重灌、库里已经是元）也要记下
	// 「054 已经来过了」，否则下一次启动会重新评估内容守卫。行为腿是
	// TestMigration054_SkipsWhenAlreadyCNY；这条结构断言管的是「为什么必须这样写」。
	if strings.Index(stmt, "END IF") > strings.Index(stmt, "INSERT INTO data_migrations") {
		t.Error("054 的哨兵 INSERT 被挪进了 IF 里 —— 跳过换算的那次启动不会留痕，" +
			"内容守卫下次还会重新成立")
	}
	// 备份必须在 UPDATE 之前（否则恢复集里已经是换算后的值，等于没备份）。
	if strings.Index(stmt, "ohlcv_daily_qfq_bak_054") > strings.Index(stmt, "SET turnover = turnover * 1000") {
		t.Error("054 先换算后备份 —— bak_054 里存的会是换算后的值，恢复集失效")
	}
}

// openTempSession 起一条「临时 schema 独占可见」的连接并建好两张临时表。
// 每个场景都要新连接：哨兵与临时表都按会话算，复用了就测不出「第二次启动」。
func openTempSession(t *testing.T, rows []turnoverOHLCVRow, withSentinel bool) *pgx.Conn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, turnoverTestDSN)
	if err != nil {
		t.Skipf("本机 PG 不可用: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	mustExec(t, conn, `SET search_path = pg_temp`)
	mustExec(t, conn, `
		CREATE TEMP TABLE ohlcv_daily_qfq (
			symbol      VARCHAR(20) NOT NULL,
			trade_date  DATE        NOT NULL,
			close       DOUBLE PRECISION NOT NULL,
			volume      DOUBLE PRECISION NOT NULL,
			turnover    DOUBLE PRECISION NOT NULL,
			PRIMARY KEY (symbol, trade_date)
		)`)
	mustExec(t, conn, `
		CREATE TEMP TABLE data_migrations (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)

	for i, r := range rows {
		if _, err := conn.Exec(ctx,
			`INSERT INTO ohlcv_daily_qfq (symbol, trade_date, close, volume, turnover)
			 VALUES ($1, CURRENT_DATE - $2::int, $3, $4, $5)`,
			r.symbol, i, r.close_, r.volume, r.turnover); err != nil {
			t.Fatalf("seed %s: %v", r.symbol, err)
		}
	}
	if withSentinel {
		if _, err := conn.Exec(ctx,
			`INSERT INTO data_migrations (name) VALUES ($1)`, turnoverMigrationName); err != nil {
			t.Fatalf("seed sentinel: %v", err)
		}
	}

	// 双保险：非限定名必须读到本用例的种子行（而不是真库的 392.6 万行）。
	// 这条不成立就绝不执行 DO 块 —— 宁可测试红，也不动共享库。
	if got := countRows(t, conn, "ohlcv_daily_qfq"); got != len(rows) {
		t.Fatalf("临时表隔离失效：ohlcv_daily_qfq 读到 %d 行，种子只有 %d 行 —— 名字没落在 pg_temp",
			got, len(rows))
	}
	return conn
}

func mustExec(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("exec %.60s...: %v", sql, err)
	}
}

func runMigration(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := conn.Exec(ctx, turnoverMigrationStmt(t)); err != nil {
		t.Fatalf("跑 054 迁移体失败: %v", err)
	}
}

// turnoverOf 读回某一行的 turnover。
func turnoverOf(t *testing.T, conn *pgx.Conn, symbol string) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var v float64
	if err := conn.QueryRow(ctx,
		`SELECT turnover FROM pg_temp.ohlcv_daily_qfq WHERE symbol = $1`, symbol).Scan(&v); err != nil {
		t.Fatalf("read turnover %s: %v", symbol, err)
	}
	return v
}

func countRows(t *testing.T, conn *pgx.Conn, table string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func tempTableExists(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ok bool
	if err := conn.QueryRow(ctx,
		`SELECT to_regclass('pg_temp.' || $1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatalf("to_regclass %s: %v", name, err)
	}
	return ok
}

func sentinelCount(t *testing.T, conn *pgx.Conn) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var n int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM pg_temp.data_migrations WHERE name = $1`,
		turnoverMigrationName).Scan(&n); err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	return n
}

// 千元态种子：turnover / (volume × close) ≈ 0.001（真库中位数 0.0010127）。
// 10 千元 = 10000 元；80 千元 = 80000 元。
var thousandCNYRows = []turnoverOHLCVRow{
	{symbol: "a054k1", close_: 10, volume: 1000, turnover: 10},
	{symbol: "a054k2", close_: 20, volume: 2000, turnover: 80},
}

// CNY 态种子：比值 ≈ 1（已是规范层单位），清库重灌就是这个形状。
var cnyRows = []turnoverOHLCVRow{
	{symbol: "a054c1", close_: 10, volume: 1000, turnover: 10000},
	{symbol: "a054c2", close_: 20, volume: 2000, turnover: 40000},
}

func TestMigration054_ConvertsThousandCNY(t *testing.T) {
	conn := openTempSession(t, thousandCNYRows, false)
	runMigration(t, conn)

	// 绝对数值断言（不从阈值推导）：千元 ×1000 = 元。
	if got := turnoverOf(t, conn, "a054k1"); got != 10000 {
		t.Errorf("a054k1 turnover = %v, want 10000（10 千元 × 1000）", got)
	}
	if got := turnoverOf(t, conn, "a054k2"); got != 80000 {
		t.Errorf("a054k2 turnover = %v, want 80000（80 千元 × 1000）", got)
	}
	if !tempTableExists(t, conn, "ohlcv_daily_qfq_bak_054") {
		t.Error("没建 ohlcv_daily_qfq_bak_054 —— 换算前没有恢复集")
	} else if bak := bakTurnover(t, conn, "a054k1"); bak != 10 {
		t.Errorf("bak_054 存的 a054k1 = %v, want 10（换算**前**的千元原值）", bak)
	}
	if n := sentinelCount(t, conn); n != 1 {
		t.Errorf("哨兵没写进 data_migrations：count=%d, want 1", n)
	}
}

func bakTurnover(t *testing.T, conn *pgx.Conn, symbol string) float64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var v float64
	if err := conn.QueryRow(ctx,
		`SELECT turnover FROM pg_temp.ohlcv_daily_qfq_bak_054 WHERE symbol = $1`, symbol).Scan(&v); err != nil {
		t.Fatalf("read bak %s: %v", symbol, err)
	}
	return v
}

// TestMigration054_RerunIsNoop 同一会话连跑两次（服务重启的形状）。
func TestMigration054_RerunIsNoop(t *testing.T) {
	conn := openTempSession(t, thousandCNYRows, false)
	runMigration(t, conn)
	first := turnoverOf(t, conn, "a054k1")
	runMigration(t, conn)

	if got := turnoverOf(t, conn, "a054k1"); got != first {
		t.Errorf("重跑后 a054k1 = %v, want 仍是 %v（×1000 不能重复生效）", got, first)
	}
	if got := turnoverOf(t, conn, "a054k2"); got != 80000 {
		t.Errorf("重跑后 a054k2 = %v, want 80000", got)
	}
}

// TestMigration054_SkipsWhenAlreadyCNY：库里已经是元（清库后用新代码重灌）⇒
// 内容守卫不放行，一行都不动。
func TestMigration054_SkipsWhenAlreadyCNY(t *testing.T) {
	conn := openTempSession(t, cnyRows, false)
	runMigration(t, conn)

	if got := turnoverOf(t, conn, "a054c1"); got != 10000 {
		t.Errorf("已是元的行被改了：a054c1 = %v, want 10000", got)
	}
	if got := turnoverOf(t, conn, "a054c2"); got != 40000 {
		t.Errorf("已是元的行被改了：a054c2 = %v, want 40000", got)
	}
	if tempTableExists(t, conn, "ohlcv_daily_qfq_bak_054") {
		t.Error("没换算却建了备份表 —— 备份应当只在真正换算的那一次出现")
	}
	if n := sentinelCount(t, conn); n != 1 {
		t.Errorf("跳过换算也要落哨兵：count=%d, want 1", n)
	}
}

// TestMigration054_SentinelBlocksStrayLotsRows 是 052 那轮审查钉住的坑在这里的
// 复刻：**迁移完成后**又落进一行千元数据（旧二进制没换 / 手工导入 / 混部）。
// 内容守卫此时重新成立（比值 0.001 < 0.01），只有哨兵能挡住「整表再 ×1000」。
func TestMigration054_SentinelBlocksStrayLotsRows(t *testing.T) {
	mixed := append(append([]turnoverOHLCVRow{}, cnyRows...),
		turnoverOHLCVRow{symbol: "a054stray", close_: 10, volume: 1000, turnover: 10})

	// ① 有哨兵：整表一动不动，哪怕内容守卫成立。
	conn := openTempSession(t, mixed, true)
	runMigration(t, conn)
	for sym, want := range map[string]float64{"a054c1": 10000, "a054c2": 40000, "a054stray": 10} {
		if got := turnoverOf(t, conn, sym); got != want {
			t.Errorf("有哨兵时 %s 被改了：got %v, want %v（哨兵必须让 054 至多生效一次）", sym, got, want)
		}
	}

	// ② 没有哨兵（同一份数据、同一条 SQL）：内容守卫放行 ⇒ 整表 ×1000。
	// 这条腿证明「挡住上面那次的是哨兵，不是内容守卫」，也就证明 ① 不是假护栏。
	conn2 := openTempSession(t, mixed, false)
	runMigration(t, conn2)
	if got := turnoverOf(t, conn2, "a054c1"); got != 10000000 {
		t.Errorf("无哨兵时 a054c1 = %v, want 10000000 —— 内容守卫放行后整表换算，"+
			"这正是哨兵要拦住的静默放大", got)
	}
}

// TestMigration054_GoalpostsThreshold 钉住准入阈值本身，而不只是「守卫在不在」。
//
// 真库实测的比值两端：千元态 max 0.003535、元态 min 0.3226。这里造两个**贴着
// 各自那一侧**的行，验证 0.01 这个绝对数值把它们分开了。只删守卫测不出阈值
// 意义 —— 阈值被改大（比如 1.0）时，元态行会被误换算，本测试必须红。
func TestMigration054_GoalpostsThreshold(t *testing.T) {
	// 千元态、比值贴着实测 max：close×volume = 1e6、turnover = 3535 ⇒ 0.003535
	conn := openTempSession(t,
		[]turnoverOHLCVRow{{symbol: "a054g1", close_: 1000, volume: 1000, turnover: 3535}}, false)
	runMigration(t, conn)
	if got := turnoverOf(t, conn, "a054g1"); got != 3535000 {
		t.Errorf("比值 0.003535 的千元行没被换算：got %v, want 3535000", got)
	}

	// 元态、比值贴着实测 min（0.3226）：close×volume = 1e6 ⇒ turnover = 322600。
	// 绝不该被当成千元。（第一版这里误填 322.6，比值其实是 0.0003226，仍是千元态
	// 的形状 —— 被本测试自己抓红，改完才对着实测边界。留在这句注释里是因为
	// 「贴边界的种子必须把比值算出来，不是把数值抄过来」这个坑值得记。）
	conn2 := openTempSession(t,
		[]turnoverOHLCVRow{{symbol: "a054g2", close_: 1000, volume: 1000, turnover: 322600}}, false)
	runMigration(t, conn2)
	if got := turnoverOf(t, conn2, "a054g2"); got != 322600 {
		t.Errorf("比值 0.3226 的元态行被误换算：got %v, want 322600 —— 阈值放得太宽了", got)
	}
}
