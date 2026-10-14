package main

// wiring_guard_test.go —— SYNC-01 的接线腿。
//
// 守什么：data-service 的启动序列必须**调用**调度器的 Start，关闭序列必须调用 Stop。
//
// 为什么需要这条守卫（而不是只测 `Scheduler.Start()` 的行为）：SYNC-01 的缺陷形状是
// 「函数写得对，但全仓零调用方」—— 生产没有，测试也没有。那种缺陷**编译期看不见、
// 行为测试也看不见**（行为测试自己会去调 Start，于是永远绿）。唯一能机器抓住它的判据
// 是装配点本身有没有这条调用。同族先例见 `cmd/analysis/wiring_guard_test.go`（K1 切片 3）。
//
// 为什么是文本扫描而不是 AST：断言对象是「main.go 里有没有这个调用」，不是函数体内部结构；
// 全文 Contains 足够。锚定的是**调用形态**（`syncHandler.StartScheduler(`），
// 所以注释里提到方法名不算违规、也不会让守卫变绿。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func readSource(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(name)
	require.NoError(t, err, "读不到 %s —— 文件改名了，守卫要跟着改", name)
	return string(raw)
}

// TestStartupStartsScheduler 钉住 SYNC-01 的修复本身：库里 active 的排程要能触发，
// 前提是进程启动时把调度器跑起来。
func TestStartupStartsScheduler(t *testing.T) {
	t.Parallel()
	mainSrc := readSource(t, "main.go")

	require.Contains(t, mainSrc, "syncHandler.StartScheduler(",
		"main 未启动调度器 ⇒ sync_schedules 里 is_active=true 的排程永远不会触发（SYNC-01）")
	require.Contains(t, mainSrc, "syncHandler.StopScheduler(",
		"main 未停止调度器 ⇒ 优雅关闭期间 cron 仍可能在 HTTP 已停后新建作业")

	// SYNC-02：worker pool 的 Stop 同样必须有调用方 —— 否则关闭时在跑的
	// 同步作业随进程一起被掐断，连「下次启动再收敛」的机会都没有。
	// 顺序约束：HTTP → scheduler → worker pool，StopWorkerPool 必须在
	// StopScheduler 之后（cron 先停，才不会往正在拆除的队列里塞新作业）。
	require.Contains(t, mainSrc, "syncHandler.StopWorkerPool(",
		"main 未停止 worker pool ⇒ 关闭时在跑的同步作业随进程一起被掐断（SYNC-02）")
	require.Greater(t, strings.Index(mainSrc, "syncHandler.StopScheduler("),
		strings.LastIndex(mainSrc, "gracefulShutdown("),
		"关闭顺序被破坏：必须先停 HTTP 再停 scheduler")
	require.Greater(t, strings.Index(mainSrc, "syncHandler.StopWorkerPool("),
		strings.Index(mainSrc, "syncHandler.StopScheduler("),
		"关闭顺序被破坏：必须先停 scheduler 再停 worker pool（SYNC-02）")

	// fail-loud 而非静默：启动失败必须进日志，不能 `_ =` 吞掉。
	require.Contains(t, mainSrc, "if err := syncHandler.StartScheduler(); err != nil {",
		"StartScheduler 的错误没有被显式处理 —— 调度器起不来时界面只会看到一片 0，无人知道它没启动")
}

// TestSyncHandlerExposesSchedulerLifecycle 钉住方法确实在 SyncHandler 上（与
// StartWorkerPool / StopWorkerPool 同一组生命周期入口），防止守卫只匹配到注释。
func TestSyncHandlerExposesSchedulerLifecycle(t *testing.T) {
	t.Parallel()
	handlersSrc := readSource(t, "sync_handlers.go")

	require.Contains(t, handlersSrc, "func (h *SyncHandler) StartScheduler() error {",
		"SyncHandler 上没有 StartScheduler —— 生命周期入口缺失")
	require.Contains(t, handlersSrc, "func (h *SyncHandler) StopScheduler() {",
		"SyncHandler 上没有 StopScheduler")
}
