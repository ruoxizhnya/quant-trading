package storage

import (
	"context"
	"fmt"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 因子挖掘留痕（D8 ③2「审计留痕」——治理层的 DB 侧形态）。
//
// 与 D4 的 factor_registration_audit（谁把哪一版接进来）互补：本表记
// 「**这一版凭什么进库**」——来源（谁生成、第几代、依据）、沙箱凭证哈希、
// 治理层两件（过拟合 / 样本外）的判定结果。知道一个因子进了库还不够，还得
// 知道它是怎么挖出来的、过了哪些检验，才叫可追溯。
//
// 一条 = 一行，(factor_name, version_hash) 主键（一个版本一条留痕）。被拒的
// 候选也留痕（VersionHash 非空即可）——「试过但没要」同样是审计信息。
//
// 写入路径过 domain.FactorMiningRecord.Validate；DB 侧用 CHECK 兜底（生成器
// 非空、来源枚举、尝试次数为正、已入库必须带绑定该版本的沙箱凭证、被拒必须
// 写明原因），连「绕过写入路径直接 INSERT」也被库拒绝（fail-closed）。

// SaveFactorMiningRecord 幂等写入一条挖掘留痕（按 factor_name + version_hash）。
//
// 同一 (名, 哈希) 重复写入是 no-op——不留痕漂移：一个定义版本只该有一天
// 「挖出它」的那条记录。
func (s *PostgresStore) SaveFactorMiningRecord(ctx context.Context, rec domain.FactorMiningRecord) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	const query = `
		INSERT INTO factor_mining_records (
			factor_name, version_hash, source_kind, generator, generation, rationale,
			num_trials, failed_trials, sandbox_hash, admitted, reject_reason,
			overfit_raw_p, overfit_adj_p, overfit_flag,
			oos_in_sample, oos_out_sample, oos_pass, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		        $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (factor_name, version_hash) DO NOTHING
	`
	if _, err := s.pool.Exec(ctx, query,
		string(rec.FactorName), rec.VersionHash, rec.SourceKind, rec.Generator,
		rec.Generation, rec.Rationale, rec.NumTrials, rec.FailedTrials,
		rec.SandboxHash, rec.Admitted, rec.RejectReason,
		rec.OverfitRawP, rec.OverfitAdjP, rec.OverfitFlag,
		rec.OOSInSample, rec.OOSOutSample, rec.OOSPass, rec.CreatedAt,
	); err != nil {
		return fmt.Errorf("save factor mining record %s@%s: %w", rec.FactorName, rec.VersionHash, err)
	}
	return nil
}

// LoadFactorMiningRecords 读全部挖掘留痕（按因子名、创建时刻升序）。
//
// 逐条重新跑 Validate（拒绝把库里的畸形行将就着返回）——与
// LoadFactorRegistrations 的「加载即复核」同款 fail-closed 立场。
func (s *PostgresStore) LoadFactorMiningRecords(ctx context.Context) ([]domain.FactorMiningRecord, error) {
	const query = `
		SELECT factor_name, version_hash, source_kind, generator, generation, rationale,
		       num_trials, failed_trials, sandbox_hash, admitted, reject_reason,
		       overfit_raw_p, overfit_adj_p, overfit_flag,
		       oos_in_sample, oos_out_sample, oos_pass, created_at
		FROM factor_mining_records
		ORDER BY factor_name, created_at
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load factor mining records: %w", err)
	}
	defer rows.Close()

	var out []domain.FactorMiningRecord
	for rows.Next() {
		var rec domain.FactorMiningRecord
		if err := rows.Scan(
			&rec.FactorName, &rec.VersionHash, &rec.SourceKind, &rec.Generator,
			&rec.Generation, &rec.Rationale, &rec.NumTrials, &rec.FailedTrials,
			&rec.SandboxHash, &rec.Admitted, &rec.RejectReason,
			&rec.OverfitRawP, &rec.OverfitAdjP, &rec.OverfitFlag,
			&rec.OOSInSample, &rec.OOSOutSample, &rec.OOSPass, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan factor mining record: %w", err)
		}
		if err := rec.Validate(); err != nil {
			return nil, fmt.Errorf("load factor mining records: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factor mining records: %w", err)
	}
	return out, nil
}
