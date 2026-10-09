package strategy

// K7 切片 1：ExternalSignal 契约单测 —— content_hash 坐标 + 校验。
//
// 关注两件事：
//   1. SignalContentHash 是「可执行字段」的确定性坐标：同一信号两次哈希一致、
//      任一可执行字段变化哈希必变、Factors（诊断）不参与哈希、时区归一；
//   2. ValidateExternalSignal fail-loud 拒绝「写下去就是脏行」的输入。
//
// 纯函数测试，无 DB 依赖。
import (
	"math"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/require"
)

// sigBase 是测试基准信号（合法）。
func sigBase() ExternalSignal {
	return ExternalSignal{
		ModelID:   "model-x",
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		Strength:  0.75,
		AsOf:      time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
		Factors:   map[string]float64{"ml_pred": 0.032},
	}
}

// ─── SignalContentHash ─────────────────────────────────────────────

func TestSignalContentHashIsDeterministic(t *testing.T) {
	a := sigBase()
	b := sigBase()
	require.Equal(t, SignalContentHash(a), SignalContentHash(b),
		"同一信号两次哈希必须一致（坐标稳定，幂等去重的前提）")
	require.Len(t, SignalContentHash(a), 64, "content_hash 应为 64 位 hex sha256")
}

func TestSignalContentHashSensitiveToActionableFields(t *testing.T) {
	base := sigBase()
	baseHash := SignalContentHash(base)

	cases := []struct {
		name   string
		mutate func(*ExternalSignal)
	}{
		{"model_id", func(s *ExternalSignal) { s.ModelID = "model-y" }},
		{"symbol", func(s *ExternalSignal) { s.Symbol = "000001.SZ" }},
		{"direction", func(s *ExternalSignal) { s.Direction = domain.DirectionShort }},
		{"strength", func(s *ExternalSignal) { s.Strength = 0.9 }},
		{"as_of", func(s *ExternalSignal) { s.AsOf = s.AsOf.AddDate(0, 0, 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.mutate(&s)
			require.NotEqual(t, baseHash, SignalContentHash(s),
				"改 %s 后哈希必须变（否则该字段不是可执行字段却被哈希忽略）", tc.name)
		})
	}
}

func TestSignalContentHashIgnoresFactors(t *testing.T) {
	a := sigBase()
	b := sigBase()
	b.Factors = map[string]float64{"ml_pred": 999.0, "conf": 0.1} // 完全不同的诊断快照
	require.Equal(t, SignalContentHash(a), SignalContentHash(b),
		"Factors 是诊断非身份，不参与哈希（同一可执行信号只应有一个坐标）")
}

func TestSignalContentHashNormalizesTimezone(t *testing.T) {
	a := sigBase()
	b := sigBase()
	// 同一瞬时，不同时区表达（+08:00 的 08:00 == UTC 的 00:00）。
	b.AsOf = time.Date(2024, 3, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.Equal(t, SignalContentHash(a), SignalContentHash(b),
		"as_of 必须归一 UTC：同一瞬时不同时区表达应哈希一致")
}

// ─── ValidateExternalSignal ────────────────────────────────────────

func TestValidateExternalSignalAcceptsValid(t *testing.T) {
	for _, dir := range []domain.Direction{
		domain.DirectionLong, domain.DirectionShort, domain.DirectionClose,
	} {
		s := sigBase()
		s.Direction = dir
		require.NoError(t, ValidateExternalSignal(s), "合法方向 %q 应通过", dir)
	}
}

func TestValidateExternalSignalRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ExternalSignal)
	}{
		{"empty model_id", func(s *ExternalSignal) { s.ModelID = "" }},
		{"empty symbol", func(s *ExternalSignal) { s.Symbol = "" }},
		{"hold direction", func(s *ExternalSignal) { s.Direction = domain.DirectionHold }},
		{"empty direction", func(s *ExternalSignal) { s.Direction = "" }},
		{"garbage direction", func(s *ExternalSignal) { s.Direction = "buy" }},
		{"NaN strength", func(s *ExternalSignal) { s.Strength = math.NaN() }},
		{"+Inf strength", func(s *ExternalSignal) { s.Strength = math.Inf(1) }},
		{"zero as_of", func(s *ExternalSignal) { s.AsOf = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := sigBase()
			tc.mutate(&s)
			require.Error(t, ValidateExternalSignal(s), "非法输入必须被拒（fail-loud）")
		})
	}
}
