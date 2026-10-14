package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// 因子登记账本（D4 因子注册表治理细则——治理库的 DB 侧形态）。
//
// 与 pkg/data.RegistrationBook（进程内账本）的关系：账本是「管」的读路径、
// 本表是它的持久投影——一条登记 = 一行（factor_name, version_hash 主键），
// 定义快照存 JSONB（让回滚后的老版本**自包含**、可复现，不依赖别处还留着
// 旧定义对象）。
//
// 四样齐全（公式 / 哈希 / 登记人 / 生效日）由**两道关卡**共同守住
// （fail-closed）：
//
//	① Go 写入路径：reg.Validate() 拒收缺样登记；
//	② DB 约束（见 Migration 053）：registrar 非空 + 内核体存在 + 状态枚举，
//	   连「绕过写入路径直接 INSERT」也被库拒绝。
//
// 哈希纪律同 D3 的 factor_definitions：写入时以 Definition.VersionHash()
// 派生值为准；加载时重新 derive 与库里存的版本比对，不符即拒载。

// SaveFactorRegistration 幂等写入一条登记（按 factor_name + version_hash）。
//
// 「原样重放同一版」= 幂等 no-op（与 D3 的 SaveFactorDefinitions 同款）：
// 同一 (名, 哈希) 重复登记不新增行、不重复留痕。改内容必然改哈希 → 新行。
func (s *PostgresStore) SaveFactorRegistration(ctx context.Context, reg domain.FactorRegistration) error {
	if err := reg.Validate(); err != nil {
		return err
	}
	defJSON, err := json.Marshal(reg.Definition)
	if err != nil {
		return fmt.Errorf("marshal factor definition %q: %w", reg.Definition.Name, err)
	}
	const query = `
		INSERT INTO factor_registrations (
			factor_name, version_hash, definition, registrar, status, registered_at
		)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)
		ON CONFLICT (factor_name, version_hash) DO NOTHING
	`
	tag, err := s.pool.Exec(ctx, query,
		string(reg.Definition.Name), reg.VersionHash, string(defJSON),
		reg.Registrar, string(reg.Status), reg.RegisteredAt,
	)
	if err != nil {
		return fmt.Errorf("save factor registration %s@%s: %w", reg.Definition.Name, reg.VersionHash, err)
	}
	if tag.RowsAffected() == 0 {
		return nil // 幂等：同一版本已在册
	}
	return s.appendRegistrationAudit(ctx, domain.RegistrationAuditEntry{
		Name:        reg.Definition.Name,
		VersionHash: reg.VersionHash,
		Action:      domain.ActionRegister,
		Actor:       reg.Registrar,
		Reason:      "",
		Detail:      fmt.Sprintf("kernel=%s caliber=%s effective_from=%s", reg.Definition.Kernel, reg.Definition.Caliber, reg.Definition.EffectiveFrom),
		At:          reg.RegisteredAt,
	})
}

// RollbackFactorRegistration 回滚某一版登记：新引用不再命中，历史引用照旧。
//
// 只有仍在册（status='active'）的版本可回滚；找不到或已回滚 → 报错
// （fail-closed）。actor 为空 → 报错（审计「谁」不可缺）。成功后追加审计留痕。
func (s *PostgresStore) RollbackFactorRegistration(ctx context.Context, name domain.FactorType, versionHash, actor, reason string, at time.Time) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("factor %q: rollback actor missing (audit trail requires 'who')", name)
	}
	const query = `
		UPDATE factor_registrations
		SET status = 'rolled_back', rolled_back_at = $3, rolled_back_by = $4, rollback_reason = $5
		WHERE factor_name = $1 AND version_hash = $2 AND status = 'active'
	`
	tag, err := s.pool.Exec(ctx, query, string(name), versionHash, at, actor, reason)
	if err != nil {
		return fmt.Errorf("rollback factor registration %s@%s: %w", name, versionHash, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("factor %q: version %s not registered as active; nothing to roll back", name, versionHash)
	}
	return s.appendRegistrationAudit(ctx, domain.RegistrationAuditEntry{
		Name:        name,
		VersionHash: versionHash,
		Action:      domain.ActionRollback,
		Actor:       actor,
		Reason:      reason,
		Detail:      "new references will no longer resolve this version; pinned (historical) references still hit",
		At:          at,
	})
}

func (s *PostgresStore) appendRegistrationAudit(ctx context.Context, e domain.RegistrationAuditEntry) error {
	const query = `
		INSERT INTO factor_registration_audit (factor_name, version_hash, action, actor, reason, detail, audited_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if _, err := s.pool.Exec(ctx, query,
		string(e.Name), e.VersionHash, string(e.Action), e.Actor, e.Reason, e.Detail, e.At,
	); err != nil {
		return fmt.Errorf("append registration audit %s@%s: %w", e.Name, e.VersionHash, err)
	}
	return nil
}

// LoadFactorRegistrations 读全部登记（按因子名、登记时刻升序）。
//
// 逐条重新 derive 哈希并与库里存的版本比对：不符 = 库被绕过写入路径改过
// → 返回 error（fail-closed），而不是「将就着用」。
func (s *PostgresStore) LoadFactorRegistrations(ctx context.Context) ([]domain.FactorRegistration, error) {
	const query = `
		SELECT factor_name, version_hash, definition, registrar, status,
		       registered_at, rolled_back_at, rolled_back_by, rollback_reason
		FROM factor_registrations
		ORDER BY factor_name, registered_at
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load factor registrations: %w", err)
	}
	defer rows.Close()

	var out []domain.FactorRegistration
	for rows.Next() {
		var reg domain.FactorRegistration
		var storedHash string
		var defJSON []byte
		if err := rows.Scan(
			&reg.Definition.Name, &storedHash, &defJSON, &reg.Registrar, &reg.Status,
			&reg.RegisteredAt, &reg.RolledBackAt, &reg.RolledBackBy, &reg.RollbackReason,
		); err != nil {
			return nil, fmt.Errorf("scan factor registration: %w", err)
		}
		if err := json.Unmarshal(defJSON, &reg.Definition); err != nil {
			return nil, fmt.Errorf("unmarshal factor definition %q: %w", reg.Definition.Name, err)
		}
		derived := reg.Definition.VersionHash()
		if derived != storedHash {
			return nil, fmt.Errorf("factor registration %q: stored version hash %s does not match derived %s — the library row was modified outside the write path",
				reg.Definition.Name, storedHash, derived)
		}
		reg.VersionHash = derived
		out = append(out, reg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factor registrations: %w", err)
	}
	return out, nil
}

// LoadFactorRegistrationAudit 读全部审计留痕（按写入序）。
func (s *PostgresStore) LoadFactorRegistrationAudit(ctx context.Context) ([]domain.RegistrationAuditEntry, error) {
	const query = `
		SELECT factor_name, version_hash, action, actor, reason, detail, audited_at
		FROM factor_registration_audit
		ORDER BY id
	`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load factor registration audit: %w", err)
	}
	defer rows.Close()

	var out []domain.RegistrationAuditEntry
	for rows.Next() {
		var e domain.RegistrationAuditEntry
		if err := rows.Scan(&e.Name, &e.VersionHash, &e.Action, &e.Actor, &e.Reason, &e.Detail, &e.At); err != nil {
			return nil, fmt.Errorf("scan factor registration audit: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factor registration audit: %w", err)
	}
	return out, nil
}
