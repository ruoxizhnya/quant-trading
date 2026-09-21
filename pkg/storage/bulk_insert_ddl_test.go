package storage

import (
	"os"
	"regexp"
	"testing"
)

// 回归：ETL 的 TableMapper 声明 13 类数据，其中 11 类的目标表此前**只存在于
// migrations/*.sql** —— 而那个目录不被任何代码执行（golang-migrate 封装已删，
// postgres.go 的注释里写明了）。后果是全新部署一跑多源同步就
// `relation does not exist` 硬失败：多源数据面在干净环境开箱不可用，
// 存量库全靠有人手工跑过那些 .sql 才活着。
//
// DDL 的唯一真相是 postgres.go 里的 migrate() 数组，所以「写入目标 ⊆ 内联 DDL」
// 必须是一条断言，而不是一句约定 —— 约定没有执法者就必然漂移（C5 正是这么来的）。
//
// 这个测试刻意**零 DB 依赖**：静态读源码就够了，CI 上不需要起 Postgres。
func TestTableMapper_TablesDeclaredInInlineDDL(t *testing.T) {
	src, err := os.ReadFile("postgres.go")
	if err != nil {
		t.Fatalf("读取 postgres.go 失败: %v", err)
	}

	re := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+([a-zA-Z_][a-zA-Z0-9_.]*)`)
	declared := make(map[string]struct{})
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		declared[m[1]] = struct{}{}
	}
	if len(declared) == 0 {
		t.Fatalf("正则没匹配到任何 CREATE TABLE —— 测试本身失效，检查 postgres.go 是否还在原位")
	}

	mapper := NewTableMapper()
	if len(mapper.tables) == 0 {
		t.Fatalf("TableMapper 为空 —— 测试本身失效")
	}

	for dataType, table := range mapper.tables {
		if _, ok := declared[table]; !ok {
			t.Errorf("data_type %q 的目标表 %q 未在内联 DDL 中声明 —— 新环境同步该类型会 relation does not exist",
				dataType, table)
		}
	}
}
