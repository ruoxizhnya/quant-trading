package storage

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 因子定义库（D3 公式化因子引擎——「库里公式」的 DB 侧形态）。
//
// 与内存注册表（pkg/data.Registry + BuiltinFactorDefinitions）的关系：
// 种子定义编译进二进制，本表是它的持久投影。写入路径幂等
// （SyncFactorDefinitions 每次计算前 upsert 当前定义集）；读取路径
// 逐条重新 derive 版本哈希并校验——库里存的哈希与定义内容不符即
// 拒绝加载（fail-closed）。「同一次回测引用固定版本哈希重现一致」
// 的承诺由 derive-not-store 保证：哈希没有独立存储入口可被篡改。

// SaveFactorDefinitions 幂等 upsert 一组因子定义（按 factor_name 主键）。
//
// 写入前逐条重新 derive 哈希并以派生值为准——调用方传进来的
// version_hash 永远不被信任（结构体里也没有这个字段，见
// domain.FactorDefinition.VersionHash 的方法形态）。
func (s *PostgresStore) SaveFactorDefinitions(ctx context.Context, defs []domain.FactorDefinition) error {
	if len(defs) == 0 {
		return nil
	}
	const query = `
		INSERT INTO factor_definitions (
			factor_name, kernel, formula, validity, code_ref, caliber,
			input_fields, effective_from, lookahead, lookback, version_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (factor_name) DO UPDATE SET
			kernel         = EXCLUDED.kernel,
			formula        = EXCLUDED.formula,
			validity       = EXCLUDED.validity,
			code_ref       = EXCLUDED.code_ref,
			caliber        = EXCLUDED.caliber,
			input_fields   = EXCLUDED.input_fields,
			effective_from = EXCLUDED.effective_from,
			lookahead      = EXCLUDED.lookahead,
			lookback       = EXCLUDED.lookback,
			version_hash   = EXCLUDED.version_hash,
			updated_at     = NOW()
	`
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return err
		}
		fields := d.InputFields
		if fields == nil {
			fields = []string{}
		}
		if _, err := s.pool.Exec(ctx, query,
			string(d.Name), string(d.Kernel), d.Formula, d.Validity, d.CodeRef,
			string(d.Caliber), fields, d.EffectiveFrom, d.Lookahead, d.Lookback,
			d.VersionHash(),
		); err != nil {
			return fmt.Errorf("save factor definition %q: %w", d.Name, err)
		}
	}
	return nil
}

// LoadFactorDefinitions 读全部因子定义（按 factor_name 升序）。
//
// 逐条重新 derive 哈希并与库内 stored 值比对：不一致 = 库被改过且
// 不是通过本写入路径——返回 error 而不是「将就着用」。
func (s *PostgresStore) LoadFactorDefinitions(ctx context.Context) ([]domain.FactorDefinition, error) {
	const query = `
		SELECT factor_name, kernel, formula, validity, code_ref, caliber,
		       input_fields, effective_from, lookahead, lookback, version_hash
		FROM factor_definitions
		ORDER BY factor_name
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load factor definitions: %w", err)
	}
	defer rows.Close()

	var defs []domain.FactorDefinition
	for rows.Next() {
		var d domain.FactorDefinition
		var storedHash string
		if err := rows.Scan(
			&d.Name, &d.Kernel, &d.Formula, &d.Validity, &d.CodeRef,
			&d.Caliber, &d.InputFields, &d.EffectiveFrom, &d.Lookahead,
			&d.Lookback, &storedHash,
		); err != nil {
			return nil, fmt.Errorf("scan factor definition: %w", err)
		}
		if d.VersionHash() != storedHash {
			return nil, fmt.Errorf("factor definition %q: stored version hash %s does not match derived %s — the library row was modified outside the write path",
				d.Name, storedHash, d.VersionHash())
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factor definitions: %w", err)
	}
	return defs, nil
}

// SyncFactorDefinitions 把一组定义 upsert 进库（幂等）。
// 供计算线启动/每次计算前调用：库里公式与进程内种子保持同源。
func (s *PostgresStore) SyncFactorDefinitions(ctx context.Context, defs []domain.FactorDefinition) error {
	return s.SaveFactorDefinitions(ctx, defs)
}
