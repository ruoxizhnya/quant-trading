package domain

import (
	"fmt"
	"strings"
	"time"
)

// ─── D4 因子注册表治理细则：登记记录 + 四样 fail-closed ────────────────────
//
// D3 把因子定义从 Go 代码搬进库（「库里公式」），D4 定「进库之后的纪律」：
//
//   - 每条**登记**必须四样齐全——公式、哈希、登记人、生效日——缺一拒收
//     （fail-closed，见 FactorRegistration.Validate）；
//   - 回滚 = 新引用不再命中该版本，历史回测仍能引到老版本（可复现性红线，
//     解析语义见 pkg/data.RegistrationBook）。
//
// 与 FactorDefinition 的分工：定义是「算什么」（内容），登记是「谁在什么
// 时候把它接进来的」（治理）。一条登记 = 一份**定义快照** + 登记人 ——
// 快照让「老版本可复现」自包含（不必依赖别处还留着旧定义对象）。

// RegistrationStatus 是登记的生命周期态。
type RegistrationStatus string

const (
	// RegistrationActive：登记生效，参与「新引用」的解析。
	RegistrationActive RegistrationStatus = "active"
	// RegistrationRolledBack：已回滚。新引用不再命中该版本；**历史引用**
	// （按版本哈希锁定）仍能引到——回滚不破坏历史回测复现。
	RegistrationRolledBack RegistrationStatus = "rolled_back"
)

// Valid reports whether the status is one of the declared kinds.
// An unknown status fails closed rather than defaulting to anything.
func (s RegistrationStatus) Valid() bool {
	switch s {
	case RegistrationActive, RegistrationRolledBack:
		return true
	default:
		return false
	}
}

// FactorRegistration 是一条登记记录。
//
// 四样（登记生效的充分必要条件）在本类型的落法：
//
//   - 公式：Definition 携带内核体（leaf/formula 的 Formula、code 的 CodeRef），
//     由 Definition.Validate() 逐型校验；pending 态无内核体，不得登记为生效；
//   - 哈希：VersionHash 是登记时记录的版本指纹，必须等于 Definition.VersionHash()
//     （与 D3 同一算法：canonical JSON 的 SHA-256）；
//   - 登记人：Registrar 非空（纯空白不算）；
//   - 生效日：Definition.EffectiveFrom（YYYY-MM-DD，字符串契约）。
type FactorRegistration struct {
	// Definition 是登记时的定义快照。改公式必须新登记一份（新哈希），
	// 老快照原样留存——这正是「回滚后老版本仍可复现」的载体。
	Definition FactorDefinition `json:"definition"`

	// VersionHash 是登记的版本指纹（四样之「哈希」）。它是**记录**而非
	// 权威——写入/加载都以 Definition.VersionHash() 为准重新 derive 比对，
	// 不符即拒收（与 D3 的 factor_definitions.version_hash 同款纪律）。
	VersionHash string `json:"version_hash"`

	// Registrar 是登记人（四样之「登记人」）：谁把这一版定义接进来的。
	Registrar string `json:"registrar"`

	Status       RegistrationStatus `json:"status"`
	RegisteredAt time.Time          `json:"registered_at"`

	// 回滚留痕（谁/何时/为什么）：三条只在 Status=rolled_back 时有值。
	RolledBackAt   *time.Time `json:"rolled_back_at,omitempty"`
	RolledBackBy   string     `json:"rolled_back_by,omitempty"`
	RollbackReason string     `json:"rollback_reason,omitempty"`
}

// Validate 对登记做四样齐全校验（fail-closed）。
//
// 缺任一样即返回带「four-required」点名的错误——调用方（存储写入路径、
// 治理解析层）据此拒收，不允许「差不多的登记」进库。
func (r FactorRegistration) Validate() error {
	// 公式（四样之一）：定义结构自洽 + 内核体存在。
	if err := r.Definition.Validate(); err != nil {
		return fmt.Errorf("registration %q: definition invalid: %w", r.Definition.Name, err)
	}
	if r.Definition.Kernel == KernelPending {
		return fmt.Errorf("registration %q: pending kernel carries no formula/code_ref; not registrable as effective (four-required: formula)", r.Definition.Name)
	}

	// 哈希（四样之二）：非空且与内容派生值一致。
	derived := r.Definition.VersionHash()
	if r.VersionHash == "" {
		return fmt.Errorf("registration %q: version_hash missing (four-required: hash)", r.Definition.Name)
	}
	if r.VersionHash != derived {
		return fmt.Errorf("registration %q: version_hash %s does not match derived %s (four-required: hash)", r.Definition.Name, r.VersionHash, derived)
	}

	// 登记人（四样之三）：非空（纯空白不算）。
	if strings.TrimSpace(r.Registrar) == "" {
		return fmt.Errorf("registration %q: registrar missing (four-required: registrar)", r.Definition.Name)
	}

	// 生效日（四样之四）：Definition.Validate 已校验格式与非空
	// （"effective_from must be YYYY-MM-DD"），此处不再重复解析。

	// 状态：只认 active / rolled_back。
	if !r.Status.Valid() {
		return fmt.Errorf("registration %q: unknown status %q", r.Definition.Name, r.Status)
	}
	return nil
}

// ─── 审计留痕（谁 / 何时 / 改了什么 / 为什么） ────────────────────────────

// RegistrationAction 是审计条目记录的动作。
type RegistrationAction string

const (
	// ActionRegister：登记（把一版定义接进库）。
	ActionRegister RegistrationAction = "register"
	// ActionRollback：回滚（让新引用不再命中该版本）。
	ActionRollback RegistrationAction = "rollback"
)

// RegistrationAuditEntry 是一条审计留痕。
//
// 「谁」= Actor，「何时」= At，「改了什么」= VersionHash（版本指纹即内容
// 指纹）+ Detail，「为什么」= Reason。
type RegistrationAuditEntry struct {
	Name        FactorType         `json:"factor_name"`
	VersionHash string             `json:"version_hash"`
	Action      RegistrationAction `json:"action"`
	Actor       string             `json:"actor"`
	Reason      string             `json:"reason"`
	Detail      string             `json:"detail,omitempty"`
	At          time.Time          `json:"at"`
}
