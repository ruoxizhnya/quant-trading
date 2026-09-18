// Package workers 提供固定大小的并发执行器（P1-6）。
//
// 它是 LEAF 包：只依赖标准库，不 import pkg/backtest 或任何兄弟子包 ——
// 这样 batch/ 与 walkforward/ 都能用它而不会有循环依赖（同一个理由让
// contracts/ 成为 leaf）。
//
// 为什么不放在 contracts/：那个包是 DTO + 接口契约，不放执行器。
package workers

import (
	"context"
	"sync"
)

// RunWorkers 用**固定数量**的 worker 并发处理一批任务（P1-6）。
//
// 为什么不能直接 `for _, job := range jobs { go f(job) }` 再拿信号量限并发：
// 那样 goroutine 数仍然等于任务数 —— 1 万个任务就是 1 万个 goroutine 同时
// 存在，只是大部分卡在信号量上。内存占用和调度开销都是 O(任务数) 而不是
// O(并发数)，信号量只治了"同时执行几个"，没治"同时存在几个"。
//
// 这里反过来：先起固定 N 个 worker，让它们从 channel 里领活。goroutine
// 总数上界是 min(workers, len(jobs))，与任务数无关。
//
// 两点约定：
//  1. **结果顺序确定**：fn 收到的是任务下标，调用方按 idx 写回结果槽位。
//     worker 之间完成先后不确定，但落位确定 —— 同一份输入跑两次，结果
//     逐位一致（P1-14 的可复现要求）。
//  2. **ctx 取消后不再领新活**（已在跑的不强杀，那是 fn 自己的责任）。
//     这让取消在 O(workers) 内收敛，而不是把队列排完。
func RunWorkers[T any](ctx context.Context, workers int, jobs []T, fn func(ctx context.Context, idx int, job T)) {
	if len(jobs) == 0 {
		return
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}

	// channel 里只传下标：任务结构体可能是几十上百字节，传值会在入队和
	// 出队各拷贝一次；传下标则只在真正处理时读一次。
	jobCh := make(chan int, len(jobs))
	for i := range jobs {
		jobCh <- i
	}
	close(jobCh)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				fn(ctx, idx, jobs[idx])
			}
		}()
	}
	wg.Wait()
}
