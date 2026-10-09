package backtest

// OBS-11：策略服务 per-day HTTP 信号 fallback 已删除的正证据。
//
// 反模式原貌：引擎每个交易日把**全量 marketData** POST 给策略服务
// （`POST /strategies/:name/signals`）要信号——慢、不可复现、把执行语义泄漏
// 到服务边界外。且 ADR-012 后策略服务 standby，服务端端点恒回 503，这条
// fallback 是**必然失败**的死路径（比「建了没接」更糟：接着，但对面拆了）。
//
// 本测试钉住删除后的契约：未注册策略 **fail-loud**——报错点名策略名、给出
// 可用清单、并标注 fallback 已移除；错误里**不得**再出现 HTTP/503 痕迹
// （那是 fallback 还活着的表现）。
import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/ruoxizhnya/quant-trading/pkg/strategy"
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func TestGetSignals_FailLoudOnUnregistered(t *testing.T) {
	eng, _, symbols := buildSyntheticEngine(t, 2, 5, 1)

	_, err := eng.getSignals(context.Background(), "no-such-strategy",
		symbols, map[string][]domain.OHLCV{}, time.Now(), nil) // miss 分支不触碰 tracker
	if err == nil {
		t.Fatal("未注册策略应 fail-loud，而不是静默或走（已删除的）HTTP fallback")
	}
	msg := err.Error()

	// 报错要点名策略与给可用清单（AI/调用方能自纠）。
	if !strings.Contains(msg, "no-such-strategy") {
		t.Errorf("错误未点名策略名：%v", err)
	}
	if !strings.Contains(msg, "not registered") {
		t.Errorf("错误未说明「未注册」：%v", err)
	}
	if !strings.Contains(msg, "OBS-11") {
		t.Errorf("错误未标注 fallback 已移除（OBS-11）：%v", err)
	}
	// fallback 的痕迹必须消失：503 是旧路径「必然失败」的信号。
	if strings.Contains(msg, "503") || strings.Contains(strings.ToLower(msg), "http") {
		t.Errorf("错误里仍有 HTTP fallback 痕迹（删除不彻底）：%v", err)
	}
}

// TestGetSignals_RegisteredStillWorks 反证腿：注册过的策略仍走本地路径
// 产出信号（删除 fallback 不得破坏正路）。用回测里注册的探针策略直接调
// getSignals，断言不报「not registered」。
func TestGetSignals_RegisteredStillWorks(t *testing.T) {
	eng, _, symbols := buildSyntheticEngine(t, 2, 5, 1)

	strat := newReconcileProbe("obs11_local_probe")
	if err := strategy.GlobalRegister(strat); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	tracker := NewTracker(1_000_000, 0.0003, 0.0001, TradingConfig{}, zerolog.Nop())
	if _, err := eng.getSignals(context.Background(), strat.Name(),
		symbols, map[string][]domain.OHLCV{}, time.Now(), tracker); err != nil {
		if strings.Contains(err.Error(), "not registered") {
			t.Fatalf("已注册策略被误判未注册: %v", err)
		}
		// 非注册类错误（如空 marketData 的内部问题）不在本测试断言范围。
	}
}
