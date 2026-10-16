package data

// market_activity 的 D4 登记演练（D5 ③3：按 D3 定义骨架 + D4 登记四样）。
//
// D4 的治理账本（RegistrationBook）要求每条登记四样齐全：公式 / 哈希 /
// 登记人 / 生效日。本文件走一遍 D5 新因子的登记路径，钉住「名册协议变更
// 之后，新因子仍然要走既有治理口径」——不因是新样板而放宽任何校验。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

// marketActivitySeedDef 取种子里的 market_activity 定义（D5 协议变更新增）。
func marketActivitySeedDef(t *testing.T) domain.FactorDefinition {
	t.Helper()
	for _, d := range domain.BuiltinFactorDefinitions {
		if d.Name == domain.FactorMarketActivity {
			return d
		}
	}
	t.Fatalf("种子定义必须含 market_activity（D5 名册协议变更）")
	return domain.FactorDefinition{}
}

// TestMarketActivityRegistration_FourItemsAndPinnedResolution 演练：
// 种子定义 → 四样校验 → 登记在册 → 新引用命中 → 历史引用按哈希锁定命中。
func TestMarketActivityRegistration_FourItemsAndPinnedResolution(t *testing.T) {
	def := marketActivitySeedDef(t)

	book := NewRegistrationBook()
	at := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)

	reg, err := book.Register(def, "D5-sample", "D5：挂新数据源（daily_basic）→ 可挖因子的样板因子", at)
	require.NoError(t, err, "四样齐全的登记必须通过")
	require.Equal(t, domain.RegistrationActive, reg.Status)
	require.Equal(t, def.VersionHash(), reg.VersionHash, "登记哈希 = 定义的内容指纹（同一算法）")
	require.Equal(t, "D5-sample", reg.Registrar)
	require.Equal(t, "2026-10-15", def.EffectiveFrom)

	// 新引用（按名解析）命中该版。
	eff, hash, ok := book.ResolveEffective(domain.FactorMarketActivity)
	require.True(t, ok)
	require.Equal(t, def.VersionHash(), hash)
	require.Equal(t, def.Formula, eff.Formula)

	// 历史引用（按 (名, 哈希) 锁定）同样命中——回滚不破坏复现的基础。
	pinned, ok := book.ResolvePinned(domain.FactorMarketActivity, hash)
	require.True(t, ok)
	require.Equal(t, def.Formula, pinned.Formula)

	// 审计留痕：谁 / 何时 / 为什么。
	audit := book.Audit()
	require.Len(t, audit, 1)
	require.Equal(t, domain.ActionRegister, audit[0].Action)
	require.Equal(t, "D5-sample", audit[0].Actor)
	require.NotEmpty(t, audit[0].Reason)

	// 重复登记同一版（同哈希）→ 拒收（改定义必须改哈希）。
	_, err = book.Register(def, "D5-sample", "重复登记", at)
	require.ErrorIs(t, err, ErrRegistrationDuplicate)
}

// TestMarketActivityRegistration_RejectsMissingItems 四样缺一拒收：
// 登记人是空串（「谁」不可缺）→ fail-closed。其余三样由
// domain.FactorRegistration.Validate 的既有测试覆盖（D4）。
func TestMarketActivityRegistration_RejectsMissingItems(t *testing.T) {
	def := marketActivitySeedDef(t)

	book := NewRegistrationBook()
	_, err := book.Register(def, "   ", "缺登记人", time.Now().UTC())
	require.Error(t, err, "缺登记人（审计的「谁」）必须拒收")
}
