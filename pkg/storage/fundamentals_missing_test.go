package storage

import (
	"context"
	"testing"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2-10 的回归测试：库里的 NULL 该读成「未知」（nil），不是 0。
//
// 修复前 GetFundamentals 用 COALESCE(pe, 0) 兜底（因为 domain.Fundamental
// 当时是 float64，扫 NULL 会报错）。代价是「这家公司没披露 PE」和
// 「PE = 0」在读出来之后一模一样 —— 而 PE=0 在估值因子眼里是"白送的股票"。
// 现在列可空、字段是指针，NULL 就扫成 nil。
func TestGetFundamentals_NullStaysNil(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	const tsCode = "TEST_MISSING_PE.SH"
	defer store.DB().Exec(ctx, "DELETE FROM stock_fundamentals WHERE ts_code=$1", tsCode)

	// 只填 PB，其余留 nil —— 模拟一期只披露了部分指标的财报。
	pb := 1.4
	require.NoError(t, store.SaveFundamental(ctx, &domain.Fundamental{
		Symbol: tsCode,
		Date:   parseDate("2024-09-30"),
		PB:     &pb,
	}))

	rows, err := store.GetFundamentals(ctx, tsCode, parseDate("2024-12-31"))
	require.NoError(t, err)
	require.NotEmpty(t, rows, "应当能读到刚才写进去的那一行")

	f := rows[0]
	assert.Nil(t, f.PE, "库里是 NULL，就该读成 nil（未知），而不是 0 —— PE=0 会被当成极便宜")
	assert.Nil(t, f.ROE, "同上")
	require.NotNil(t, f.PB, "PB 有值，不该是 nil")
	assert.InDelta(t, 1.4, *f.PB, 0.0001)
}
