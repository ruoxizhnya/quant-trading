package backtest

// AUD-60 的引擎侧护栏：**分类**必须钉住。
//
// 为什么值得单独一条：`Engine.store` 是具体类型（`*storage.PostgresStore`）而不是
// 接口，所以「真正走到 checkCalendarExists 返回 false 那条分支」需要真库 ——
// 引擎侧的这条分支在冻结树里没有端到端腿。但它**给出的分类**不需要真库就能断言，
// 而分类恰好就是缺陷本身：定成 INVALID_INPUT 时，handler 会把它报成 500
// （codeForStatus(500) = "internal"），调用方读到的是一句「服务器内部错误」。
//
// 「分类 → 客户端看到的状态」由 internal/httpserver 的映射表单独覆盖
// （DATA_QUALITY → 409，且「未知即 500」有对照腿）。
// 真环境那一段由 A/B 覆盖：请求一个库里没有日历的区间（如 2030 年）应得 409。

import (
	"errors"
	"testing"

	apperrors "github.com/ruoxizhnya/quant-trading/pkg/errors"
)

func TestErrTradingCalendarNotSynced_IsDataQualityNotInvalidInput(t *testing.T) {
	err := errTradingCalendarNotSynced()

	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("应当是 AppError，实际 %T", err)
	}

	if appErr.Code != apperrors.ErrCodeDataQuality {
		t.Fatalf("分类应当是 DATA_QUALITY（缺的是库里的数据，不是请求写错了），实际 %s", appErr.Code)
	}
	// 反证腿：这个前置失败**不能**被当成「你传错了」。
	if appErr.Code == apperrors.ErrCodeInvalidInput {
		t.Fatal("分类退化回了 INVALID_INPUT —— 那会让调用方去改一个本来就对的请求")
	}

	// 运维面：分类错了会连带污染日志级别。handler 的 5xx 收口走 log.Error，
	// 于是一个调用方自己能解的问题会出现在错误告警里。
	if appErr.Operation != "RunBacktest" {
		t.Fatalf("操作名应当保留，实际 %q", appErr.Operation)
	}
}
