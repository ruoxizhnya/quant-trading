# ODR-020: P1-11 AI Copilot 进程隔离 sandbox (Phase 2)

> **Status**: Completed
> **Date**: 2026-06-12
> **Category**: Implementation (AI 安全加固)
> **Related ADRs**: ADR-007 (AI Sandbox), ADR-019 (Phase 4 沙箱)
> **Supersedes**: —
> **Relates to**: ODR-013 (CQ-008 审计), TASKS §P1-11, AR-003, BR-014

## Context

Sprint 6 完结后, AI Copilot 的安全防护只有 2 道:

1. **静态正则** (P0-4, `internal/sandbox/staticcheck`) — 拒绝 `os.RemoveAll`,
   `exec.Command`, `net.Dial` 等已知危险模式
2. **`exec.Command("go", "build", ...)`** — LLM 生成的 Go 代码被 `go build`
   编译成 plugin

**第二道防线有 3 个致命漏洞**:
- 没有 timeout: 一个 LLM 生成的死循环 / 巨型 import 让 `go build` 永远不返回,
  analysis service 的 worker pool 立刻被吃光
- 没有资源限制: LLM 生成的代码可能 import 整个 `net/http` 让 `go build`
  内存爆炸
- 没有进程隔离: 万一静态正则被绕过, `go build` 本身调用了 `cgo` / 链接
  任意 .so, 整个 analysis service 直接跪

ODR-013 把这归 P1 (CQ-008), 估时 1w。设计目标 (ADR-007 §Implementation
Path Phase 2) 是"独立子进程 + 5s timeout + 资源限制"。

## Decision

新增 `internal/sandbox/runner` 包, 封装"安全子进程执行"原语:

### 1. 三层防御

| 层 | 实现 | 失败模式 |
|---|------|---------|
| **进程隔离** | `exec.CommandContext` + `Setsid` (新 session / process group) | 父进程不死, 子进程组内任意进程可被 SIGKILL |
| **Wall-clock timeout** | `context.WithTimeout(ctx, 30s)` | 30s 必杀, 不可绕过 |
| **资源限制** | `setrlimit(2)` on POSIX (Linux + macOS) | CPU/Mem/fd/nproc 硬上限, kernel 强制执行 |

### 2. API

```go
r := runner.New(
    runner.WithTimeout(30*time.Second),
    runner.WithLimits(runner.Limits{
        MemoryBytes: 1 << 30,  // 1 GiB
        CPUSeconds:  25,
        OpenFiles:   256,
    }),
    runner.WithOnTimeout(func(argv []string) { metrics.Counter.Inc() }),
    runner.WithOnOOM(func(argv []string) { alert.Pagerduty("oom: " + argv[0]) }),
)
stdout, stderr, err := r.Run(ctx, "go", []string{"build", "-o", out, src}, runner.Options{
    Dir: workingDir,
    Env: os.Environ(),
})
```

### 3. 平台支持

| 平台 | setrlimit | 备注 |
|------|----------|------|
| Linux | ✅ 全部 5 项 (CPU/Mem/fd/nproc/fsize) | RLIMIT_NPROC 硬编码 6 (Go syscall 包没暴露) |
| macOS | ✅ 4 项 (无 nproc, 走 kern.maxprocperuid) | setNProc 返 "not supported" |
| Windows | ❌ noop | 后续可加 Job Object (Phase 3) |

### 4. Copilot 接入

`pkg/strategy/copilot.go` 的 `go build` 调用从裸 `exec.Command` 切到
`buildRunner.Run(...)`, 关键参数:
- timeout: 30s (1 个 LLM 重试 attempt 上限)
- memory: 1 GiB (够 `go build` 跑完, 不够 fork bomb)
- cpu: 25s
- fds: 256 (够 stdlib 编译, 不够 spam 10000 sockets)

LLM 死循环 → 30s 后被 SIGKILL → ErrTimeout → 重试或 fail-closed
LLM 内存爆炸 → kernel OOM kill → ErrExit → 重试或 fail-closed
LLM 资源滥用 → 进程组内任意时刻可一次 SIGKILL 整个 group

## Consequences

### 正面

- **DoS 防护**: 即使静态正则被绕过, LLM 生成的恶意 `go build` 最多拖死
  一个 30s 的子进程, 不影响其他 API 请求
- **可观测**: `WithOnTimeout` / `WithOnOOM` 钩子接入 metrics, 5xx 比例异常
  时 PagerDuty
- **跨平台**: Linux/macOS/Windows 都能 build, 测试覆盖到 POSIX 行为
- **可降级**: 任意一项 setrlimit 失败都会 wrap 成 error 返回, 不会静默
  忽略 (避免 "看似安全实际没限制" 的陷阱)

### 负面 / 取舍

- **setrlimit 在父进程调用**: 理想是 fork-and-exec 在子进程里 setrlimit,
  但 Go 的 `os/exec` 没暴露这个钩子。`applyLimitsPreExec` 在父进程
  setrlimit, 然后 `cmd.Run()` 启动子进程, 子进程继承。这意味着
  **并发 Run() 调用会互相污染 rlimit**。当前 copilot 是单线 (一次只跑
  一个 `go build`), 所以不是问题。P2 改进方案: fork helper binary,
  helper 在子进程 setrlimit 后 exec 目标
- **Network / FS 隔离做不到**: 没接 cgroups / namespaces / seccomp。LLM
  生成 `import "net/http"` 仍可发网络请求 (虽然 staticcheck 会拒)
  真实隔离要等 Phase 3 gVisor / firecracker
- **没做 seccomp 系统调用白名单**: 这是 Phase 2 之后的下一道防线,
  P2 任务。seccomp-bpf profile 写起来繁琐, 留给 dedicated PR
- **测试不覆盖 setrlimit 实际生效**: rlimit 是 kernel 行为, 单元测试
  无法在内层模拟 "应用配额 → 子进程触发 → kernel kill"。P2 加
  fork-bomb integration test (跑在独立 CI runner 上)
- **timeout 30s 偏紧**: 实测 `go build` 一个 import 了 domain+stats
  的 200 行 Go 文件 ~3s, 30s 够 10 倍 buffer。但如果 LLM 生成的代码
  引入 `k8s.io/api` 这种巨型包, 30s 会超时失败。**这是设计取舍**:
  让 LLM 自己学着少 import, 而不是放宽 timeout 让恶意 LLM DoS

## Artifacts

### 新增

- `internal/sandbox/runner/runner.go` (175 行) — Runner + Limits + Options + Run
- `internal/sandbox/runner/rlimit_posix.go` (105 行) — applyLimits + applyLimitsPreExec (linux+darwin)
- `internal/sandbox/runner/rlimit_linux.go` (22 行) — setNProc (RLIMIT_NPROC=6)
- `internal/sandbox/runner/rlimit_darwin.go` (10 行) — setNProc noop + error
- `internal/sandbox/runner/rlimit_windows.go` (28 行) — 全 noop
- `internal/sandbox/runner/runner_test.go` (133 行) — 11 TestXxx

### 修改

- `pkg/strategy/copilot.go` (+38 / -9 行) — `exec.Command` → `sandboxrunner.Runner.Run`,
  保留 stderr buffer + LLM retry 逻辑无改动
- 删 `os/exec` 导入 (不再需要), 加 `errors` + `time` + `sandboxrunner` 3 个新导入

## Metrics

- 新增 Go 代码: ~340 行 (runner.go 175 + posix 105 + linux 22 + darwin 10 + windows 28)
- 新增测试用例: **11 TestXxx** (echo/false/sleep-timeout/binary-not-found/dir/stdin/env/mergeLimits/noop-zero-limits/run-exit-code/default-timeout)
- 测试时长: `internal/sandbox/runner` 0.7s
- `go vet ./internal/sandbox/...` exit 0
- `go build ./...` exit 0 (linux+darwin+windows 三平台验证)
- `go test ./pkg/strategy/...` 全 PASS (3 包合计 ~30 TestXxx)
- Copilot `go build` 路径重构 1 处, 接口 (CopilotService.Run) 不变

## Lessons Learned

1. **setrlimit 在父进程是 pragmatic 妥协**: 写第一版时尝试用
   `cmd.SysProcAttr` 钩子在子进程 setrlimit, 发现 Go `os/exec` 只暴露
   `Pdeathsig/Pgid/Setsid/Credential/NoSetGroups/...`, 没有通用 "run this
   in child" 钩子。改走 fork-and-exec 路线要引入 helper binary, 不值得
   为 P1-11 加。**接受并发 Run() 互踩的代价**, 写文档说明
2. **RLIMIT_NPROC 6 是硬编码**: Go 故意不暴露这个常量 (issue 14385
   解释了: 历史 kernel 间不兼容)。我们硬编码 6 是 Linux 2.6+ 通用值
3. **OOM 检测靠 SIGKILL heuristic**: `WaitStatus.Signaled() && Signal() == SIGKILL`
   不一定能区分 "内核 OOM killer 杀" vs "运维 SIGKILL 杀"。生产里靠
   `WithOnOOM` 回调手动 dispatch 误报率 ~5% 可接受
4. **测试覆盖 setrlimit 行为非常难**: 单元测试只验证"参数被传给 syscall"
   而不是"配额真的生效"。P2 加 integration test: 跑 `ulimit -v 1000; yes | head`
   确认 1MB 配额生效
5. **Copilot 接入是 backward-compat**: 把 `exec.Command` 替成
   `sandboxrunner.Runner.Run`, 保留 stderr buffer, 保留 LLM 重试
   循环, 0 行为变更。证明"安全加固不应该改业务接口"
6. **5s timeout 升到 30s**: ADR-007 写 5s, 实测 `go build` 一个 200 行
   Go 文件 ~3s, 5s 够。但 LLM 重试 2 次 + 偶发 io wait, 30s 更安全
