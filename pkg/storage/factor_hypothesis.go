package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 因子的假设来源（P2-3）。
//
// factor_cache 存的是因子的**数值**，这里存的是因子的**理由**。缺了后者，
// 就没法回答"这个因子凭什么有效" —— 而"在数据上试出来的相关性"和
// "有经济学机制支撑的效应"是两回事，前者换个时间段就散。

// SaveFactorHypothesis 写入或更新一个因子的假设来源。
//
// 用 factor_name 做主键：假设是因子级的，一个因子只有一条当前假设。
// 改写时是**覆盖**（连同 updated_at），不是追加历史 —— 需要历史的话
// 那是另一回事（现在没这个需求，别为假想的需求建表）。
func (s *PostgresStore) SaveFactorHypothesis(ctx context.Context, h domain.FactorHypothesis) error {
	if h.FactorName == "" {
		return fmt.Errorf("factor hypothesis: factor_name is required")
	}
	if h.Hypothesis == "" {
		return fmt.Errorf("factor hypothesis: hypothesis is required (空假设等于没记录)")
	}
	if h.SourceKind == "" {
		h.SourceKind = domain.FactorSourceAdHoc
	}

	const query = `
		INSERT INTO factor_hypothesis (factor_name, source_kind, hypothesis, reference)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (factor_name) DO UPDATE SET
			source_kind = EXCLUDED.source_kind,
			hypothesis  = EXCLUDED.hypothesis,
			reference   = EXCLUDED.reference,
			updated_at  = NOW()
	`
	if _, err := s.pool.Exec(ctx, query,
		string(h.FactorName), h.SourceKind, h.Hypothesis, nullableStr(h.Reference),
	); err != nil {
		return fmt.Errorf("failed to save factor hypothesis: %w", err)
	}
	return nil
}

// GetFactorHypothesis 取一个因子的假设来源。
//
// 两个返回都为空时表示"没记录过" —— 调用方要如实上报，不要拿内置表
// 悄悄兜底（那样会让人误以为这条假设被记录过、可被引用）。
// 内置表在 domain.BuiltinFactorHypotheses，由调用方决定是否回退。
func (s *PostgresStore) GetFactorHypothesis(ctx context.Context, name domain.FactorType) (*domain.FactorHypothesis, error) {
	const query = `
		SELECT factor_name, source_kind, hypothesis, COALESCE(reference, ''), updated_at
		FROM factor_hypothesis WHERE factor_name = $1
	`
	h := &domain.FactorHypothesis{}
	var ref string
	var updated time.Time
	err := s.pool.QueryRow(ctx, query, string(name)).Scan(
		&h.FactorName, &h.SourceKind, &h.Hypothesis, &ref, &updated)
	if err != nil {
		if err.Error() == "no rows in result set" ||
			strings.Contains(err.Error(), "no rows in result set") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get factor hypothesis: %w", err)
	}
	h.Reference = ref
	return h, nil
}

// ListFactorHypotheses 列出全部已记录的因子假设（按因子名排序）。
func (s *PostgresStore) ListFactorHypotheses(ctx context.Context) ([]domain.FactorHypothesis, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT factor_name, source_kind, hypothesis, COALESCE(reference, '')
		FROM factor_hypothesis ORDER BY factor_name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list factor hypotheses: %w", err)
	}
	defer rows.Close()

	var out []domain.FactorHypothesis
	for rows.Next() {
		var h domain.FactorHypothesis
		var ref string
		if err := rows.Scan(&h.FactorName, &h.SourceKind, &h.Hypothesis, &ref); err != nil {
			return nil, fmt.Errorf("failed to scan factor hypothesis: %w", err)
		}
		h.Reference = ref
		out = append(out, h)
	}
	return out, rows.Err()
}

// SeedBuiltinFactorHypotheses 把内置因子的假设写进库（P2-3）。
//
// 只在库里还没有该因子记录时写 —— 已经有人（AI 挖掘链路）写过的不覆盖，
// 否则每次启动都会把人写的假设冲掉。
func (s *PostgresStore) SeedBuiltinFactorHypotheses(ctx context.Context) (int, error) {
	n := 0
	for _, h := range domain.BuiltinFactorHypotheses {
		const query = `
			INSERT INTO factor_hypothesis (factor_name, source_kind, hypothesis, reference)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (factor_name) DO NOTHING
		`
		tag, err := s.pool.Exec(ctx, query,
			string(h.FactorName), h.SourceKind, h.Hypothesis, nullableStr(h.Reference))
		if err != nil {
			return n, fmt.Errorf("failed to seed hypothesis for %s: %w", h.FactorName, err)
		}
		if tag.RowsAffected() > 0 {
			n++
		}
	}
	if n > 0 {
		s.logger.Info().Int("seeded", n).Msg("Factor hypotheses seeded")
	}
	return n, nil
}

// nullableStr 把空串转成 NULL。
//
// 与 experiments.go 的 nullString 方向相反（那个是 *string → string）。
// 空串当 NULL 存，读回来再折成空串 —— 否则"没写出处"和"出处是空字符串"
// 在库里是同一个值，而前者才是真实情况。
func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
