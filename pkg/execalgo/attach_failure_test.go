// K17-R1：挂载失败口径统一的单元测试（TASKS.md K17-R1 ③ 要求 4）。
//
// 覆盖：
//   - IsKnownStyle 静态判据（信号校验期拦未知算法名的依据）；
//   - 四类挂载失败各一条：未知算法 / 父单号重复 / Schedule 非法 / Σ不等；
//   - ClassifyAttachError 把四类（+ 其他）分类到正确 Kind——两条下单主
//     路径共用这一份判定，禁止各自字符串匹配 error。
//
// Σ不等（ErrQtySumMismatch）的说明：Attach 公开 API 不可达——计划由
// allocateQty 产出、Σ 恒等于父单量，该分支是纵深防御（未来任何产出
// 计划的改动一旦破坏不变量，sentinel 仍会把失败送到同一个分类器）。
// 故该类以「sentinel 直接喂分类器」钉住；其余三类经真实 Attach 触发。
package execalgo_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/execalgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsKnownStyle 是信号校验期静态判据的正反例。
func TestIsKnownStyle(t *testing.T) {
	assert.True(t, execalgo.IsKnownStyle("twap"))
	assert.True(t, execalgo.IsKnownStyle("vwap"))
	assert.False(t, execalgo.IsKnownStyle("twp"), "拼错的算法名必须被判假（K17-R1 的病根）")
	assert.False(t, execalgo.IsKnownStyle("iceberg"))
	assert.False(t, execalgo.IsKnownStyle(""), "空串不是算法（默认路径的判据）")
	assert.False(t, execalgo.IsKnownStyle("TWAP"), "大小写敏感——不静默纠错，误配置就该被点名")
}

// TestClassifyAttachError_FourKinds 经真实 Attach 触发三类 + sentinel
// 直喂第四类，断言分类器输出（K17-R1：四类各一条测试）。
func TestClassifyAttachError_FourKinds(t *testing.T) {
	d := execalgo.NewDispatcher()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

	t.Run("unknown_style", func(t *testing.T) {
		spec := dispatchParent("PO-K1", "000001.SZ", 1000, start)
		spec.Style = "twp" // 典型拼错
		_, err := d.Attach(context.Background(), spec)
		require.Error(t, err)
		assert.ErrorIs(t, err, execalgo.ErrUnknownAlgo)
		assert.Equal(t, execalgo.AttachFailureUnknownStyle, execalgo.ClassifyAttachError(err))
	})

	t.Run("duplicate_parent", func(t *testing.T) {
		spec := dispatchParent("PO-K2", "000001.SZ", 1000, start)
		_, err := d.Attach(context.Background(), spec)
		require.NoError(t, err)
		_, err = d.Attach(context.Background(), spec)
		require.Error(t, err)
		assert.ErrorIs(t, err, execalgo.ErrDuplicateParent)
		assert.Equal(t, execalgo.AttachFailureDuplicateParent, execalgo.ClassifyAttachError(err))
	})

	t.Run("schedule_invalid", func(t *testing.T) {
		spec := dispatchParent("PO-K3", "000001.SZ", 0, start) // Qty<=0 → validateParent 拒
		_, err := d.Attach(context.Background(), spec)
		require.Error(t, err)
		assert.ErrorIs(t, err, execalgo.ErrInvalidSchedule)
		assert.Equal(t, execalgo.AttachFailureInvalidSchedule, execalgo.ClassifyAttachError(err))
	})

	t.Run("qty_sum_mismatch", func(t *testing.T) {
		// 公开 API 不可达（allocateQty 保证 Σ==父单量），以 sentinel 直喂
		// 钉住分类器：未来该分支的任何触发者都会落到同一 Kind。
		err := fmt.Errorf("execalgo: parent PO-K4 plan qty sum %v != parent qty %v: %w", 999, 1000, execalgo.ErrQtySumMismatch)
		assert.ErrorIs(t, err, execalgo.ErrQtySumMismatch)
		assert.Equal(t, execalgo.AttachFailureQtySumMismatch, execalgo.ClassifyAttachError(err))
	})

	t.Run("other_and_nil", func(t *testing.T) {
		spec := dispatchParent("", "000001.SZ", 1000, start) // 空父单号 → 无 sentinel，归 Other
		_, err := d.Attach(context.Background(), spec)
		require.Error(t, err)
		assert.NotErrorIs(t, err, execalgo.ErrUnknownAlgo)
		assert.Equal(t, execalgo.AttachFailureOther, execalgo.ClassifyAttachError(err))

		assert.Equal(t, execalgo.AttachFailureKind(""), execalgo.ClassifyAttachError(nil),
			"err==nil 返回空 Kind（调用方只在 err!=nil 时调用）")
	})
}
