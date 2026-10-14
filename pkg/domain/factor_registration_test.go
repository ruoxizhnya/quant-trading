package domain

import (
	"strings"
	"testing"
	"time"
)

func validRegistration() FactorRegistration {
	def := validFormulaDef() // momentum 公式型（factor_def_test.go 同包复用）
	return FactorRegistration{
		Definition:   def,
		VersionHash:  def.VersionHash(),
		Registrar:    "alice",
		Status:       RegistrationActive,
		RegisteredAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC),
	}
}

// ─── 四样齐全（D4 ③2）：公式 / 哈希 / 登记人 / 生效日，缺一拒收 ───

func TestFactorRegistration_Validate_FourRequiredOK(t *testing.T) {
	if err := validRegistration().Validate(); err != nil {
		t.Fatalf("complete registration must validate, got: %v", err)
	}
}

func TestFactorRegistration_Validate_MissingFour(t *testing.T) {
	cases := []struct {
		name    string // 缺哪一样
		mutate  func(r *FactorRegistration)
		wantErr string
	}{
		{
			name: "缺公式（pending 无内核体）",
			mutate: func(r *FactorRegistration) {
				r.Definition = FactorDefinition{
					Name:          FactorSize,
					Kernel:        KernelPending,
					Caliber:       CaliberZScore,
					EffectiveFrom: "2026-10-14",
					Lookahead:     "trade_date",
				}
				r.VersionHash = r.Definition.VersionHash()
			},
			wantErr: "four-required: formula",
		},
		{
			name:    "缺公式（公式型空公式）",
			mutate:  func(r *FactorRegistration) { r.Definition.Formula = ""; r.VersionHash = r.Definition.VersionHash() },
			wantErr: "requires formula",
		},
		{
			name:    "缺哈希（空哈希）",
			mutate:  func(r *FactorRegistration) { r.VersionHash = "" },
			wantErr: "four-required: hash",
		},
		{
			name:    "哈希与内容不符",
			mutate:  func(r *FactorRegistration) { r.VersionHash = strings.Repeat("a", 64) },
			wantErr: "four-required: hash",
		},
		{
			name:    "缺登记人（空）",
			mutate:  func(r *FactorRegistration) { r.Registrar = "" },
			wantErr: "four-required: registrar",
		},
		{
			name:    "缺登记人（纯空白）",
			mutate:  func(r *FactorRegistration) { r.Registrar = "   " },
			wantErr: "four-required: registrar",
		},
		{
			name: "缺生效日（空）",
			mutate: func(r *FactorRegistration) {
				r.Definition.EffectiveFrom = ""
				r.VersionHash = r.Definition.VersionHash()
			},
			wantErr: "effective_from must be YYYY-MM-DD",
		},
		{
			name: "生效日格式错",
			mutate: func(r *FactorRegistration) {
				r.Definition.EffectiveFrom = "14/10/2026"
				r.VersionHash = r.Definition.VersionHash()
			},
			wantErr: "effective_from must be YYYY-MM-DD",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRegistration()
			tc.mutate(&r)
			err := r.Validate()
			if err == nil {
				t.Fatalf("expected rejection containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestFactorRegistration_Validate_UnknownStatus(t *testing.T) {
	r := validRegistration()
	r.Status = "limbo"
	if err := r.Validate(); err == nil {
		t.Fatal("unknown registration status must fail closed")
	}
}

// 哈希是内容指纹：改内容不改哈希（重放老哈希）必须被认作「哈希与内容不符」。
func TestFactorRegistration_Validate_StaleHashRejected(t *testing.T) {
	r := validRegistration()
	stale := r.VersionHash
	r.Definition.Formula = "close / ts_delay(close, 10) - 1" // 改了公式
	r.VersionHash = stale                                    // 却没换版本指纹
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "four-required: hash") {
		t.Fatalf("stale hash after content change must be rejected as hash mismatch, got: %v", err)
	}
}
