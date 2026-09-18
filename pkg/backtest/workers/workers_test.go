package workers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// P1-6：goroutine 数必须与任务数解耦。
//
// 修之前是「每任务一个 goroutine + 信号量限并发」—— 信号量只限制了同时
// **执行**几个，goroutine 却已经全部建出来了。1000 个任务就是 1000 个
// goroutine 同时存在。

// TestRunWorkers_GoroutineCountIsBounded 直接断言**并发存在的 goroutine 峰值**。
//
// 只看"总耗时"是测不出这个 bug 的：1000 个 goroutine 卡在信号量上，
// 耗时和 4 个 worker 一样，但内存和调度开销是 O(任务数)。
func TestRunWorkers_GoroutineCountIsBounded(t *testing.T) {
	const (
		tasks     = 500
		workerCap = 4
	)

	var inFlight, peak int64
	jobs := make([]int, tasks)
	for i := range jobs {
		jobs[i] = i
	}

	RunWorkers(context.Background(), workerCap, jobs,
		func(_ context.Context, _ int, _ int) {
			cur := atomic.AddInt64(&inFlight, 1)
			for {
				old := atomic.LoadInt64(&peak)
				if cur <= old || atomic.CompareAndSwapInt64(&peak, old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond) // 保证有重叠，否则测不出峰值
			atomic.AddInt64(&inFlight, -1)
		})

	if got := atomic.LoadInt64(&peak); got > int64(workerCap) {
		t.Fatalf("并发峰值 = %d，超过 worker 上限 %d —— goroutine 数与任务数没解耦", got, workerCap)
	}
	if got := atomic.LoadInt64(&peak); got < 2 {
		t.Fatalf("并发峰值只有 %d，说明压根没并发起来（测试失效）", got)
	}
}

// 结果必须按输入下标落位，与完成先后无关 —— 这是 P1-14 可复现的前提。
func TestRunWorkers_ResultsFollowInputOrder(t *testing.T) {
	jobs := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	out := make([]string, len(jobs))

	var mu sync.Mutex
	RunWorkers(context.Background(), 3, jobs,
		func(_ context.Context, idx int, job string) {
			// 故意让完成顺序与输入顺序相反
			time.Sleep(time.Duration(len(jobs)-idx) * time.Millisecond)
			mu.Lock()
			out[idx] = job
			mu.Unlock()
		})

	for i, want := range jobs {
		if out[i] != want {
			t.Errorf("out[%d] = %q, want %q —— 结果没按下标落位", i, out[i], want)
		}
	}
}

func TestRunWorkers_EveryJobRunsExactlyOnce(t *testing.T) {
	jobs := make([]int, 200)
	for i := range jobs {
		jobs[i] = i
	}

	var count int64
	var mu sync.Mutex
	seen := map[int]int{}

	RunWorkers(context.Background(), 7, jobs, func(_ context.Context, idx int, job int) {
		atomic.AddInt64(&count, 1)
		mu.Lock()
		seen[job]++
		mu.Unlock()
	})

	if got := atomic.LoadInt64(&count); got != int64(len(jobs)) {
		t.Fatalf("执行了 %d 次，want %d", got, len(jobs))
	}
	for k, v := range seen {
		if v != 1 {
			t.Errorf("任务 %d 被执行了 %d 次", k, v)
		}
	}
}

// 边界：空任务、worker 数超过任务数、worker 数为 0。
func TestRunWorkers_EdgeCases(t *testing.T) {
	RunWorkers(context.Background(), 4, nil, func(context.Context, int, int) {
		t.Error("空任务不该调用 fn")
	})

	var zero int64
	RunWorkers(context.Background(), 0, []int{1, 2, 3}, func(_ context.Context, _ int, _ int) {
		atomic.AddInt64(&zero, 1)
	})
	if zero != 3 {
		t.Errorf("workers=0 应退化成 1 并照样跑完，got %d 次", zero)
	}

	var n int64
	RunWorkers(context.Background(), 100, []int{1, 2, 3}, func(_ context.Context, _ int, _ int) {
		atomic.AddInt64(&n, 1)
	})
	if n != 3 {
		t.Fatalf("workers(100) > jobs(3) 时应全部跑完，got %d", n)
	}
}

// ctx 取消后不再领新活（已在跑的不强杀）。
func TestRunWorkers_StopsTakingNewJobsAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	jobs := make([]int, 200)
	for i := range jobs {
		jobs[i] = i
	}

	var n int64
	RunWorkers(ctx, 1, jobs, func(_ context.Context, _ int, _ int) {
		atomic.AddInt64(&n, 1)
		if atomic.LoadInt64(&n) == 3 {
			cancel()
		}
		time.Sleep(time.Millisecond)
	})

	if got := atomic.LoadInt64(&n); got >= int64(len(jobs)) {
		t.Fatalf("取消后应停止领新活，却跑完了全部 %d 个", got)
	}
}
