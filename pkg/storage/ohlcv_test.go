package storage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetClosesOn 验证 P1-7 新增的批量收盘价查询。
//
// 它是为了替代因子归因里「逐票调 GetOHLCV」的 N+1 模式而加的，所以最要紧
// 的两点是：① 一次调用拿回所有票；② **没行情的票不出现在结果里** ——
// 调用方靠这一点判断缺失，如果塞 0 进去会被读成"白送的股票"。
func TestGetClosesOn(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	const sym = "TEST_CLOSES.SH"
	defer store.DB().Exec(ctx, "DELETE FROM ohlcv_daily_qfq WHERE symbol=$1", sym)

	day := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	_, err := store.DB().Exec(ctx, `
		INSERT INTO ohlcv_daily_qfq (symbol, trade_date, open, high, low, close, volume)
		VALUES ($1, $2, 10, 11, 9, 10.5, 1000)`, sym, day)
	require.NoError(t, err)

	got, err := store.GetClosesOn(ctx, []string{sym, "NO_SUCH_STOCK.SH"}, day)
	require.NoError(t, err)
	assert.Equal(t, 10.5, got[sym])

	_, ok := got["NO_SUCH_STOCK.SH"]
	assert.False(t, ok, "没行情的票不该出现在结果里")

	// 别的日期查不到（这天没插数据）。
	other, err := store.GetClosesOn(ctx, []string{sym}, day.AddDate(0, 0, 1))
	require.NoError(t, err)
	assert.Empty(t, other, "换一天就该查不到")

	// 空列表：不查库，返回空 map 而不是报错。
	empty, err := store.GetClosesOn(ctx, nil, day)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}
