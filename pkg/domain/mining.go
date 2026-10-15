package domain

import (
	"fmt"
	"time"
)

// FactorMiningRecord 是一条挖掘候选的治理留痕（D8 ③2「审计留痕」）。
//
// 与 D4 的 RegistrationAuditEntry 的分工：审计条目记「谁把哪一版接进来」，
// 本记录记「**这一版是怎么挖出来的、过了哪些检验**」——来源（谁生成、第几
// 代、依据）、哈希（版本指纹 + 沙箱凭证）、验证记录（过拟合/样本外结果）。
// 三者合起来才叫「可追溯」：知道一个因子进了库，还得知道它凭什么进。
//
// 以 (factor_name, version_hash) 为键：一个版本一条留痕。被拒的候选也留痕
// （VersionHash 非空即可——公式连解析都没过的候选没有「版本」，不进本表）。
type FactorMiningRecord struct {
	FactorName  FactorType `json:"factor_name"`
	VersionHash string     `json:"version_hash"`

	// 来源（审计「从哪来」）。
	SourceKind string `json:"source_kind"`
	Generator  string `json:"generator"`
	Generation int    `json:"generation"`
	Rationale  string `json:"rationale,omitempty"`

	// 过拟合校正的输入：尝试次数含失败（先例 NumTrials 教训）。
	NumTrials    int `json:"num_trials"`
	FailedTrials int `json:"failed_trials"`

	// 沙箱凭证哈希（= 版本哈希，绑定该定义）；通过沙箱才有值。
	SandboxHash string `json:"sandbox_hash,omitempty"`

	Admitted     bool   `json:"admitted"`
	RejectReason string `json:"reject_reason,omitempty"`

	// 验证记录（治理层两件的输出）。
	OverfitRawP  float64 `json:"overfit_raw_p"`
	OverfitAdjP  float64 `json:"overfit_adj_p"`
	OverfitFlag  bool    `json:"overfit_flag"`
	OOSInSample  float64 `json:"oos_in_sample"`
	OOSOutSample float64 `json:"oos_out_sample"`
	OOSPass      bool    `json:"oos_pass"`

	CreatedAt time.Time `json:"created_at"`
}

// Validate 校验留痕自洽（fail-closed）：名字在冻结名册、版本哈希是 sha-256、
// 生成器与来源非空、尝试次数为正；**已入库的**记录必须带绑定该版本的沙箱
// 凭证（无凭证不得是 admitted），**被拒的**必须写明原因。
func (r FactorMiningRecord) Validate() error {
	if _, ok := ParseFactorType(string(r.FactorName)); !ok {
		return fmt.Errorf("factor mining record: name %q not in frozen registry", r.FactorName)
	}
	if len(r.VersionHash) != 64 {
		return fmt.Errorf("factor mining record %q: version_hash must be a 64-char sha-256 hex", r.FactorName)
	}
	if r.Generator == "" {
		return fmt.Errorf("factor mining record %q: generator missing (audit trail requires 'who generated')", r.FactorName)
	}
	switch r.SourceKind {
	case FactorSourceAI, FactorSourceLiterature, FactorSourceSupplyChain, FactorSourceAdHoc:
	default:
		return fmt.Errorf("factor mining record %q: unknown source_kind %q", r.FactorName, r.SourceKind)
	}
	if r.NumTrials <= 0 {
		return fmt.Errorf("factor mining record %q: num_trials must be positive", r.FactorName)
	}
	if r.Admitted && r.SandboxHash != r.VersionHash {
		return fmt.Errorf("factor mining record %q: admitted record must carry a sandbox attestation bound to the version (%q vs %q)",
			r.FactorName, r.SandboxHash, r.VersionHash)
	}
	if !r.Admitted && r.RejectReason == "" {
		return fmt.Errorf("factor mining record %q: rejected record must state why (reject_reason)", r.FactorName)
	}
	return nil
}
