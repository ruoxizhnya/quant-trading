package data

// DATA-06：FetchIndexConstituents 的落库错误必须上抛。
//
// 旧形态：SaveIndexConstituentBatch 的 error 只 logger.Warn 后继续，函数返回
// (constituents, nil) —— 「Tushare 取到了、一行都没写进去」与「写成功了」在
// HTTP 面上不可区分，而这条路径正是 DATA-01 灌 index_constituents 的通道。
// 本文件钉住新契约：落库失败 = 整个请求失败（错误上抛，原始错误可追溯）；
// 反证腿：落库成功时取数结果照常返回并如实送达 store。
//
// 口径同 supply_chain.go 的先例（其测试注释点名本函数是吞错反模式、
// 「新代码不许继承」）—— 本单就是把这条旧路径收进同一口径。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
)

// indexWeightStubBody 两期快照：600000 只在第一期（已退出），600036 两期都在。
// normalize 后应为 2 只成分股 —— 「取到 2 条」在下方逐条断言里可核对。
const indexWeightStubBody = `{"code":0,"msg":"","data":{"fields":["index_code","con_code","trade_date","weight"],"items":[
	["000300.SH","600000.SH","20241031",1.5],
	["000300.SH","600036.SH","20241031",2.5],
	["000300.SH","600036.SH","20241129",2.7]
]}}`

// failingIndexConstituentStore 只让 SaveIndexConstituentBatch 失败；
// SaveRawIngest 成功，保证 call() 的归档路径不干扰被测的错误来源。
type failingIndexConstituentStore struct {
	TushareStore
	err error
}

func (s *failingIndexConstituentStore) SaveIndexConstituentBatch(_ context.Context, _ []*domain.IndexConstituent) error {
	return s.err
}

func (s *failingIndexConstituentStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// recordingIndexConstituentStore 记下落库调用（反证腿：成功路径的读数照常送达 store）。
type recordingIndexConstituentStore struct {
	TushareStore
	rows []*domain.IndexConstituent
}

func (s *recordingIndexConstituentStore) SaveIndexConstituentBatch(_ context.Context, rows []*domain.IndexConstituent) error {
	s.rows = append(s.rows, rows...)
	return nil
}

func (s *recordingIndexConstituentStore) SaveRawIngest(_ context.Context, _ *storage.RawIngest) error {
	return nil
}

// TestFetchIndexConstituents_PropagatesSaveError 是 DATA-06 的红腿（先红后绿）：
// 把生产码的 error 重新吞回 logger.Warn ⇒ 本测试红。
func TestFetchIndexConstituents_PropagatesSaveError(t *testing.T) {
	store := &failingIndexConstituentStore{err: errors.New("db on fire")}
	client := newSupplyChainTestClient(stubTushare(t, indexWeightStubBody, 200), store)

	got, err := client.FetchIndexConstituents(context.Background(), "000300.SH", "")
	require.Error(t, err, "落库失败必须向上传播（不许 Warn 后返回 nil error）")
	require.Contains(t, err.Error(), "db on fire", "原始错误必须可追溯（%w 链保留在错误里供日志）")
	require.Contains(t, err.Error(), "fetched 2 but saved 0",
		"「取到 N 条 / 落库 0 条」必须写进错误本身")
	require.ErrorIs(t, err, ErrIndexConstituentPersistFailed,
		"落库失败必须带哨兵，调用方据此区分失败环节")
	require.Nil(t, got, "落库失败时不得把未落库的行当成结果返回")
}

// TestFetchIndexConstituents_PersistedOnSuccess 是反证腿：同一 stub 返回成功时
// 读数照常 —— 上抛改造不许把成功路径一起弄坏。
func TestFetchIndexConstituents_PersistedOnSuccess(t *testing.T) {
	store := &recordingIndexConstituentStore{}
	client := newSupplyChainTestClient(stubTushare(t, indexWeightStubBody, 200), store)

	got, err := client.FetchIndexConstituents(context.Background(), "000300.SH", "")
	require.NoError(t, err)
	require.Len(t, got, 2, "取到 2 只（600000 / 600036，按 con_code 去重合并快照）")
	require.Len(t, store.rows, 2, "落库调用必须真的发生且逐行送达（不是 Warn 掉）")

	bySymbol := map[string]domain.IndexConstituent{}
	for i, r := range store.rows {
		require.NotNil(t, r, "行 %d 不应为 nil", i)
		bySymbol[r.Symbol] = *r
	}
	require.Equal(t, "000300.SH", bySymbol["600000.SH"].IndexCode)

	first, _ := time.Parse("20060102", "20241031")
	require.Equal(t, first, bySymbol["600000.SH"].InDate)
	require.True(t, bySymbol["600000.SH"].OutDate.Equal(first.AddDate(0, 0, 1)),
		"已退出的 out_date = 最后出现期次日（DATA-01 语义不得被本单改动）")
	require.True(t, bySymbol["600036.SH"].OutDate.IsZero(), "仍在最新期 ⇒ OutDate 零值")
	require.InDelta(t, 2.7, bySymbol["600036.SH"].Weight, 1e-9)
}
