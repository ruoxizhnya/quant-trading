---
status: active
last-verified: 2026-09-25
verified-by: 代码审查（2026-09-16）+ 产品重构讨论；P0-4 落地复核（2026-09-17）；P2-9wire / P2-9f / P2-10 / P1-5 / P2-12 落地（2026-09-18）；ODR-065 AUD-01~18 全关（2026-09-21/22）；AUD-19~34 全部有裁决（2026-09-22，AUD-34 裁决保留、其余关闭）；AUD-20/21/22 落地 + 顺带登记 AUD-37（2026-09-22）；AUD-24/25/26 沙箱跨平台落地 + 顺带登记并落地 AUD-38（2026-09-22）；AUD-35/36/37 配置一致性落地 + 顺带登记 AUD-39 / AUD-40（2026-09-22）；AUD-39/40 落地（k8s env 口径 + 值对齐 + 消灭占位符 + 部署护栏检查 5~8；`v.Sub` 缺段防护）+ 顺带登记 AUD-41 / AUD-42（2026-09-22）；AUD-41/42 落地（SPEC 的 `## Configuration` 段整段订正 + doc 护栏第二项检查；`pkg/testutil` 的 DSN 收敛到 `storage.BuildDSN` + 全仓单实现结构护栏）+ 顺带登记 AUD-43 / AUD-44（2026-09-22）；AUD-43/44/45 落地（AUD-43 前提订正后**裁决保留** `pkg/testutil` 并归并入 AUD-45；AUD-45 = 统一盘点 13 个零导入者包 + 新增 `internal/repoguard` 结构性护栏；AUD-44 = 三份入口文档补 R1 frontmatter + doc 护栏第三项检查）+ 顺带登记 AUD-46 / AUD-47（2026-09-22）；AUD-46/47 落地（AUD-46 = 删除 `pkg/metrics`（ADR-017 §1 的竞争实现，四个核心指标由 `pkg/observability` 实现）+ `internal/repoguard` 新增 `retiredPackages` 退役负向断言，`pkg/decimal` 裁决保留并标注「待采用」；AUD-47 = `docs/TEST.md` §5–§7 内容复核 —— 沙箱限制改实测值 30s/1 GiB、覆盖率目标标注为「不是门禁」、前端与 e2e 用例数改实测），2026-09-22；P2-13 接真库取证（`pkg/validation/live_backtest_integration_test.go`，实测「验证器链在真数据上否掉策略」）+ 顺带登记 AUD-49 / AUD-50（sync 的 cancel 与重启恢复两项缺陷），2026-09-23；AUD-50 落地（`pkg/sync` 补 `Queue.CleanupStaleRunning` + 从 `WorkerPool.Start` 调用 + `cmd/analysis` 补上文档承诺却从未存在的启动期一半 + `internal/repoguard` 函数级接线护栏，其第一版对目标形态是瞎的、经破坏验证修正为 `file:function`；AUD-49 仍未修），2026-09-23；AUD-49 落地（sync 的取消两半都修 —— 无条件 `UpdateSyncJob` 退役、换成 `UpdateSyncJobIfStatus` 条件写（`WHERE id=$1 AND status = ANY(...)`）且 `from` 为空报错；`RetryLater`/`CompleteJob`/`FailJob`/`Dequeue`/`CleanupStaleRunning` 全部改为条件写；`WorkerPool` 补 `jobID → cancelFunc` 注册表 + per-job ctx（原来是 `context.Background()`），`cmd/data:NewSyncHandler` 接上 `SetRunningCanceller` 并由 repoguard 钉住调用点；新增 `pkg/sync/cancel_test.go` 13 条 + `pkg/storage/sync_jobs_conditional_test.go` 6 条**真库**测试；破坏验证 S1/S2 分别只红「行」与「执行器」各自那一层）+ 顺带登记 AUD-51（测试前置条件与断言读的不是同一个东西 —— `TestHasOHLCVData` 硬编码 `600000.SH` 而前置只检查表非空，局部同步必红；同类潜伏成员 3 个）+ AUD-52（e2e 套件的前置条件不再蕴含断言 —— :8084 指向已退役的 execution 服务，永远不可能绿；strategy API 期望 200 实测 401），2026-09-23；**AUD-55 / AUD-53 / AUD-54 一次关掉（2026-09-24）** —— AUD-55（根因）= 引擎对已持仓位重复下单（`computeEffectiveTarget` 的抵扣只在 `PendingQty > 0` 时生效 × 无状态策略每天对 top-N 发 `Long`）**已修**：抵扣改为**无条件** + 已持仓**实时读 tracker**（不再信 `tp.ActualQty` 缓存）+ 新增 `reconcileTargetPosition` 对齐三处**引擎外改持仓**（止损/止盈平仓、拆股、退市强平）；AUD-53（表现）= 资金侧 4.82pp → **0.76pp**、阶梯转**收敛**、零拒单；价格侧 10.31pp **归因到绝对金额约束**（一手 = 100 × 价格 vs 固定资金）、**非引擎缺陷**；AUD-54 = 归档层 6 条已删配置引用**逐条加时点/现状注记**（护栏一字未动，`--include-archive` 退出码 **1 → 0**）；三条回归护栏 + 破坏验证（**三条同时变红**）+ 顺带登记 **AUD-56**（`pkg/ai/pipeline` 有一条测试靠**外网可达性**才能结束 —— `go build` 会联网解析不存在的 import，墙内不通就挂到超时；**A/B/A/B 四轮交替**才排除「与本轮改动相关」这个误判）；**AUD-56 / AUD-51 落地（2026-09-25）** —— AUD-56 = 把那条「靠外网可达性才能结束」的测试改成**正面证据 + 反证**（`buildDir` 里放带 `replace` 的 go.mod 指向本地模块，编译成功本身即是「buildDir 被用上」的证据；换成空目录必须失败）+ `t.Setenv("GOPROXY","off")`，并给生产侧 `go build` 子进程加 `CommandContext` + 120s 上界（601s 挂死 → 6.18s 通过；破坏 `buildCmd.Dir` 后 0.93s 变红且不挂起）；AUD-51 = `pkg/storage` 四个测试改为**自灌自证**（`TestHasOHLCVData` / `TestGetTradingDays` / `TestIsTradingDay` / `TestGetTradingDates` 自己写数据自己清理，**取消前置**而非修补前置；顺带订正登记时的一处误判 —— `TestGetTradingDays` 读的是 `ohlcv_daily_qfq`、缺的是**区间**，表名对不上的只有 `TestIsTradingDay` / `TestGetTradingDates`；`skipIfNoSeedData` 收敛到 `TestGetAllStocks` 一个同源用法）；同时订正 `docs/guides/local-dev.md` 四处漂移（test 文件数 186 → 实测 **273**、`.gitignore` 的 `*_test.go` 警告失效、`cmd/ai` 已于 2026-09-18 删除却仍在教怎么跑、已知陷阱里「需要预置数据」那条随自灌自证同步）；**AUD-51 真库实测补完（2026-09-25，原生 PG 17.5，本机 Docker Desktop 不可用时的替代路径）**：四个测试在**全新空库**上全部 PASS（`pkg/storage` 整包 `ok 24.4s`），破坏验证（把自灌与断言脱钩）→ 四个测试**同时变红** —— AUD-51 由 🔶 翻 ✅
status-legend: "✅ 已完成 / 🔶 进行中 / ⬜ 待做 / ⛔ 阻塞（有未解除的前置）" —— 见下方「状态总览」
---

# TASKS — 未完成项

> **本文件只放未完成项。** 完成即删除（历史见 `archive/TASKS-history.md`，217KB）。
> 上限 5 页，超出说明该拆项目了。

---

## 当前方向

产品定位已重构为：**AI 实验员操作确定性底座做研究，验证器（被调用，不自主循环）独立证伪，人在异步审阅台上看「质疑清单 + 路径形状」，只对校准后的概率下注。**

三条主线，优先级即下列顺序：

1. **止血** — 让现有回测数字变得可信（P0）
2. **通回路** — 让 AI 真正能操作底座跑完一次循环（P0/P1）
3. **加数据** — 宏观/跨境、产业链（P1/P2）

### 状态总览（2026-09-25 复核）

**状态标记**：`✅ 已完成`（附日期） / `🔶 进行中` / `⬜ 待做` / `⛔ 阻塞`（有未解除的前置）。

| 批次 | 项数 | ✅ | 🔶 | ⬜ | ⛔ | 说明 |
|---|---|---|---|---|---|---|
| P0 | 6 | 6 | 0 | 0 | 0 | P0-1 ~ P0-6 全关（2026-09-17） |
| P1 | 16 | 16 | 0 | 0 | 0 | 含 P1-1 —— 子项 1a/1b/1c 均已完成 |
| P2 | 19 | 17 | 0 | 2 | 0 | ⬜ P2-1 / P2-2；**P2-8 复核完成（2026-09-23，通道定性 2026-09-24）** —— 结论是「口径确实实质性影响结果」，而**hfq 落库的判据已换过**：根因 AUD-55（引擎对已持仓位重复下单）**已修并钉成护栏**，资金侧的敏感度随之塌掉（4.82pp → 0.76pp、阶梯转收敛、零拒单）；剩下的价格侧差异被确认为**绝对金额约束**（一手 = 100 × 价格，而资金固定），**不是引擎缺陷** —— 由此得到「hfq 价不能直接用固定资金跑回测」这条结论，**落不落 hfq 要按新判据重新裁决**（见文末「P2-8 取证说明」）；P2-13 已解阻并落地 |
| AUD-01~18 | 18 | 18 | 0 | 0 | 0 | ODR-065 登记项全关；AUD-18 裁决为**分阶段退役**（引出 AUD-32/33） |
| AUD-19~38 | 20 | 20 | 0 | 0 | 0 | ✅ AUD-19 / AUD-23 / AUD-27 / AUD-28 / AUD-29 / AUD-30 / AUD-31 / AUD-32 / AUD-33 / AUD-20 / AUD-21 / AUD-22（2026-09-22）+ **AUD-34**（裁决：保留）+ **AUD-24 / AUD-25 / AUD-26 / AUD-38**（2026-09-22）+ **AUD-35 / AUD-36 / AUD-37**（2026-09-22，配置一致性三项） |
| AUD-39~40 | 2 | 2 | 0 | 0 | 0 | ✅ **AUD-39**（k8s 的 `DATABASE_*`/`REDIS_URL` 口径 + 值对齐 + 消灭 `${...}` 字面量占位符 + 部署护栏检查 5~8）+ **AUD-40**（`v.Sub` 缺段返回 nil → panic 防护），2026-09-22 |
| AUD-41~42 | 2 | 2 | 0 | 0 | 0 | ✅ **AUD-41**（`docs/SPEC.md` 的 `## Configuration` 段整段订正 —— 如实描述三份真实配置文件 + 策略 YAML 真实 schema；`tools/check_doc_links.py` 加第二项检查「文档里引用的 config/ · deploy/ 路径必须存在」，**第一版只认反引号、对目标形态是瞎的，已修正**）+ **AUD-42**（`pkg/testutil` 的 DSN 收敛到 `storage.BuildDSN` + 全仓「只有一处 DSN 拼装」结构护栏），2026-09-22 |
| AUD-43~45 | 3 | 3 | 0 | 0 | 0 | ✅ **AUD-43**（`pkg/testutil` 零导入者 —— 前提订正后裁决**保留**并归并入 AUD-45）+ **AUD-45**（统一盘点 **13 个**零导入者包 + 新增 `internal/repoguard` 结构性护栏「pkg/ internal/ 下的包必须有消费者，否则进白名单并写理由」，**范围从源码推导、白名单自带 stale 检测**）+ **AUD-44**（`docs/SPEC.md` / `docs/TEST.md` / `docs/ADR.md` 补 R1 frontmatter + `check_doc_links.py` 第三项检查「入口层文档必须带 R1 三字段」，**范围从 `docs/README.md` 的链接推导**），2026-09-22 |
| AUD-46~47 | 2 | 2 | 0 | 0 | 0 | ✅ **AUD-46**（删除 `pkg/metrics` —— 它是 ADR-017 §1 的**竞争实现**，那四个核心指标由 `pkg/observability` 实现，两者同时接线会重复注册 `backtest_duration_seconds` 并 panic；`internal/repoguard` 新增 **`retiredPackages` 退役负向断言**；`pkg/decimal` 裁决**保留**并标注「待采用」）+ **AUD-47**（`docs/TEST.md` §5–§7 内容复核 —— 沙箱限制 5s/100MB → 实测 **30s/1 GiB**、覆盖率目标标注为「**不是门禁**」（CI 不设阈值）、`format.test.ts` 8→**28** 例、e2e 3 套件 22 例 → 实测 **17 spec / 160 例**），2026-09-22 |

| AUD-48 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-48**（三处 Dockerfile 的 `COPY` 源路径已被删掉的目录 —— `cmd/analysis/static`（AUD-33 退役）与 `cmd/strategy/generated_strategies`（全仓零引用）；补 `check_deploy_consistency.py` 检查 9「Dockerfile 的 COPY 源路径必须存在」），2026-09-22。**它不是审查登记项，是本次完整部署时撞出来的**：镜像根本构建不出来，症状是「compose 定义了 6 个服务、实际只起得来 3 个」 |

| AUD-49~50 | 2 | 2 | 0 | 0 | 0 | ✅ **AUD-50**（重启后 `running` 的 job 永久搁浅 —— `pkg/sync` 补 `Queue.CleanupStaleRunning` 并**从 `WorkerPool.Start` 调用**（结构上不可能忘）；`cmd/analysis` 补上文档承诺却从未存在的**启动期**那一半；新增 `internal/repoguard` 的**函数级**接线护栏 —— ⚠️ **它的第一版对目标形态是瞎的**（钉文件而非函数，原 bug 下也是绿的），已修正为 `file:function` 并重做破坏验证），2026-09-23。✅ **AUD-49**（`POST /api/sync/jobs/:id/cancel` 对 `running` 任务无效 —— **两半都修**：**行**改成条件写（`UpdateSyncJobIfStatus` 是唯一入口，`WHERE id=$1 AND status = ANY(...)`，`UpdateSyncJob` 整个退役）+ **执行器**补 per-job ctx 与 `jobID → cancelFunc` 注册表，由 `cmd/data:NewSyncHandler` 接上 `workerPool.Cancel`；新增**真库**测试证明条件真的在 SQL 里，破坏验证 S1/S2 分别只红各自那一层），2026-09-23。两项都是 2026-09-23 打通数据同步时**撞出来的**，不是审查登记项。机制、取证与修法见本文件末尾「AUD-49 / AUD-50 登记说明」与「AUD-49 落地说明」 |

| AUD-51 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-51**（测试的**前置条件与断言读的不是同一个东西** —— 于是红/绿都跟被测代码无关）。**已于 2026-09-25 关闭，含真库实测 + 破坏验证**。修法：四个测试一律改为**自灌自证** —— 自己写数据、自己断言、自己清理，于是**不再需要任何前置**（问题从根上消失，而不是把前置改得「更准」）。逐条：① `TestHasOHLCVData` 自己灌一只 `TEST_HASDATA_001.SH` 再问 `HasOHLCVData`（原来断言写死 `600000.SH`，前置只查「表非空」）；② `TestGetTradingDays` 在自己灌的区间上断言，并把「`len(days) <= 22`」这条弱断言换成真正的区间不变量「结果不得越界」；③ `TestIsTradingDay` / `TestGetTradingDates` 改为灌 **`trading_calendar`**（1990-01-02 真 / 1990-01-03 假 —— 落在任何现实同步区间之外，不打架也不误删真实数据），后者顺带断言 `is_trading_day = TRUE` 过滤真的生效（原来只断言 `len > 0`）。**订正登记时的一处误判**：`TestGetTradingDays` 读的其实是 `ohlcv_daily_qfq`（`pkg/storage/ohlcv.go:146`，DISTINCT trade_date），**不是**登记说明里写的 `trading_calendar`（它的问题在「区间的具体月份」，不在表名）；表名对不上的是另外两个（`pkg/storage/calendar.go:91` / `:115`）。`skipIfNoSeedData` 只剩 `TestGetAllStocks` 一个使用者 —— 那里前置（stocks 非空）与断言（`len >= 1`）**同源**，是唯一合法用法，注释已写明这个前提。**真库实测（2026-09-25，原生 PG 17.5）**：四个测试在**全新空库**上全部 PASS（`TestHasOHLCVData` 0.30s / `TestGetTradingDays` 0.67s / `TestIsTradingDay` 0.29s / `TestGetTradingDates` 0.68s），`pkg/storage` 整包 `ok 24.4s`。**修复前的对照**：同一组测试在空库上只会 `SKIP`（`skipIfNoSeedData` 见表空即跳）—— 「跳过」也是绿，却什么都没验，是「跑红」之外的**第二种不可信信号**。**破坏验证**：把自灌与断言脱钩（自灌的 symbol 加后缀、日历日期 `+1 天`）→ **四个测试全部变红**，`TestIsTradingDay` 精确报出「标为交易日的 1990-01-02 必须返回 true」失败；还原后回到绿、`grep SABOTAGE` 零残留。静态核验：`go build ./...` / `go vet ./pkg/storage/` 全绿；自灌用的 `SaveOHLCVBatch` / `SaveTradingCalendarBatch` 与同文件里早已通过的两个测试**是同一组调用**。2026-09-23，**撞出来的**（完整跑测试时红，且**与本轮改动无关**：改的是 `sync_jobs`，实测 `600000.SH` 在库里 0 行）。修法说明见文末 |

| AUD-54 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-54**（**CI 的文档步骤本来就是红的，与任何一轮改动无关** —— `python tools/check_doc_links.py --include-archive` **退出码 1**，6 条「不存在的配置路径引用」全部指向 `config/ai-service.yaml`：`docs/archive/IMPLEMENTATION_PLAN.md:746`、`docs/archive/migration-phase3-to-phase4.md:171/449/479`、`docs/archive/odr/odr-009-code-doc-audit.md:156`、`docs/archive/tasks-phase-2.md:26`。成因是两件事撞在一起：**P2-5（2026-09-18）删了 `config/ai-service.yaml`**，而 **AUD-41（2026-09-22）新加的「文档引用的 config/ · deploy/ 路径必须存在」这一项被套到了归档层**。在 HEAD 的干净副本上复现过同样的 6 条、同样的退出码 1 —— **不是本轮引入的**。裁决未定，两条路选一：① 把那项检查的范围收到常青层+活跃层（归档是历史记录，引用的文件当时确实存在，与「导航坏链」不是一回事）；② 逐条订正归档文档（但那是在改写历史记录）。**动护栏前先裁决** —— 本项目对「改护栏」的规矩是「假护栏比没护栏更糟」。**✅ 已裁决（2026-09-24）：走 ② —— 逐条订正归档文档，护栏一个字没动。** 订正原则是「**不改写历史，只加时点与现状注记**」：6 处引用各自补一句「该文件已于 2026-09-18（TASKS P2-5）删除」；其中 `odr-009` 那条本是 2026-05-06 的审计快照，**保留**原来的「✅ 存在」并标明「审计快照」，旁边追加「后续：已删除」—— 过去的事实与现在的事实并存，谁都不被抹掉。用的是护栏**自带**的否定词豁免机制（其注释明说「历史行会合法地引用已删文件 —— TASKS.md 的『已完成』行几乎全是这个形状」），所以这不是绕护栏，而是把归档行改成它本来就该有的形状。**结果**：`python tools/check_doc_links.py --include-archive` 退出码 **1 → 0**，三项检查全绿。**破坏验证**：去掉其中一处注记 → 护栏**精确报出** `docs/archive/tasks-phase-2.md:26 -> config/ai-service.yaml`；恢复注记后回到绿 —— 说明那几行确实被看着、放行它们的正是注记本身。2026-09-23，做 P2-8 时**撞出来的** |

| AUD-53 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-53**（**回测结果对价格水平量级与资金量级都不稳健** —— 把每只票的价格乘一个正数常数（收益率序列逐点不变，`pkg/data/tushare_hfq_test.go` 有断言），同一个策略 / 同一批票 / 同一个引擎，总收益动 **21.6pp**（1M 资金）/ **24.9**（100M）；**只改资金**（1M→100M）也能让 qfq 自己的收益动 **20.7pp**，且**不收敛**（1e8→1e9 差 5.17pp、1e9→1e10 差 8.80pp）。已排除的两个归因：① **整手取整不是主因** —— 资金放大 100 倍差异不收缩；② **`detectRegime` 不是通道** —— `pkg/risk/regime_scale_invariance_test.go` 实测 regime 判定对统一缩放 14/14 窗口严格不变、对每票各自缩放 14/14 窗口完全一致（**上一轮写下的「最像的嫌疑人」已被自己的测试推翻**）。**根因已定位到代码行**：`computeEffectiveTarget`（`engine_daily.go:399`）只在 `PendingQty > 0` 时才抵扣，配上一个**无状态**策略（每天对 top-N 发 `Long`）→ 每天重发一次全量买单、每天被 `insufficient cash` 拒一次 → 订单一侧成为主通道。**该缺陷本身独立登记为 AUD-55**。取证：`pkg/data/tushare_adjustment_integration_test.go`（真库 + 真 Tushare + 真引擎，四腿对照 + 均匀缩放对照腿）、`pkg/backtest/scale_invariance_probe_test.go`（合成矩阵五腿 + 资金阶梯 + 重复下单）、`pkg/risk/regime_scale_invariance_test.go`（证伪 regime）。**✅ 定性关闭（2026-09-24，AUD-55 修复后重测合成数据）** —— 两侧的通道**不是同一个东西**，这是本轮最有价值的更正：**① 资金侧**：敏感度 4.82pp → **0.76pp**，资金阶梯从「不收敛（跳幅 4.38 / 0.44 / 5.17 / 8.80 pp、每档 777~1005 拒单）」转为**收敛（最大跳幅 0.76pp、高端残差 0.0004pp、零拒单）** → **AUD-55 就是这条通道**，已修并钉成护栏。**② 价格侧**：差异**没消失**（合成 4.22 → 10.31pp；真数据 21.6pp 一数**尚未重跑**），但通道被确认为**绝对金额约束**（一手 = 100 × 价格，而资金固定）—— **不是引擎缺陷**。并且**推翻了原来的归因**「通道在把不同票的价格放在一起比大小的环节上」（那个候选 regime 早已被证伪，本轮才补上替代解释）：per-symbol 缩放（= hfq 腿的真实形状，常数实测 1.7~181）**只把高基准的那几只推出可交易集合**（合成 9/20 只被抬到一手、最大超买 ×68），而均匀缩放是全体一起推（5/20、×3.41），所以前者凶得多。修复前那个重复下单缺陷恰好把这条效应**冲淡**了（让买得起的票买过头、收益虚高），10.31pp 因此是**更真实**的数字。**③ 对 P2-8 的直接影响**：**hfq 价不能直接用固定资金跑回测** —— hfq 价 = qfq 价 × 每股常数，单位已经不是「元」。要么资金按同一基准缩放，要么接受票池成分被改写。**21.6pp 里含着一块与「前视偏差」无关的成分**，不能读成「前视偏差值 21.6 个百分点」。**④ 未做**：真库那一套（`tushare_adjustment_integration_test.go`）**尚未重跑校准** —— 本地无真库（Docker 起不来）。该文件的两条断言只钉**方向**、不钉数值，文件头已标注。2026-09-23，做 P2-8 时**撞出来的**，不是审查登记项 |

| AUD-55 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-55**（**引擎对已持仓位重复下单** —— `momentum` 是**无状态**策略：`pkg/strategy/examples/momentum.go:219` 对 top-N **每天**发 `DirectionLong`、从不发 `Close`，且 `portfolio` 参数只用来取日期（`:111`）**不读持仓**；「已持有什么」这件事因此完全落在引擎的 `TargetPositions` 上。而 `updateTargetPositionAfterTrade`（`engine_daily.go:616`）在全额成交后置 `PendingQty = TargetQty − ActualQty = 0`，`computeEffectiveTarget`（`engine_daily.go:399`）却**只在 `PendingQty > 0` 时**才做「目标 − 已持」抵扣 → 恰好达标（`== 0`）时按**全量目标**再买一遍；仓位超目标后 `PendingQty < 0`，抵扣**依然不生效** → **每天重发一次全量买单、每天被 `insufficient cash` 拒一次**。实测：3 只票一直留在 top-N 且**关掉出场**后，60 个交易日里每票仍被买 **5~6 次**、拒单 **103** 笔、运行期间零平仓（当时叫 `TestEngineRebuysSymbolsThatStayInTarget`，已按修复翻转）；合成 20 票场景里拒单在资金 1e6~1e10 **每一档都是 777~1005 笔**，而 1e10 时抬一手 **0** 只、相对量化仅 3e-5 → 拒单与量化、与价格水平都无关。**修复必须连带处理一个耦合点**：`processStopLosses` 直接对 tracker 平仓却**不更新 `tp.ActualQty`**，所以「无条件抵扣」会让 tp 读到过期持仓、止损平仓后再也接不回来。**✅ 已修（2026-09-24，人类裁决后）**：**①** `computeEffectiveTarget` 的抵扣改成**无条件**，且**已持仓改为每次实时从 tracker 读**、不再信 `tp.ActualQty` 这个缓存 —— 理由是「能改持仓的路径不止 `executeSignalTrade`」（止损/止盈平仓、拆股、退市强平都直接落在 tracker 上），逐一补同步是「靠记得」、漏一处就退化回原 bug，读真值则结构上不可能漏（AUD-50 的同一教训）；**②** 三处**引擎外改持仓**之后调新增的 `reconcileTargetPosition` 把那份对外可见的账对齐（`processStopLosses` / `processCorporateActions` 的拆股分支 / `forceCloseDelisted`）。**回归护栏**：`TestEngineDoesNotRebuySymbolsThatStayInTarget`（每票恰好买 **1** 次、拒单 **0** 笔）、`TestBacktestCapitalLadderConverges`（只改资金 1e6→1e10 全程**零拒单**且阶梯收敛）、矩阵测试新增第五条**尺度不变腿**（价格与资金同比例缩放：差 0.09pp、成交逐笔一致）。**破坏验证**（2026-09-24 重做）：把抵扣条件改回「`PendingQty <= 0` 就返回全量目标」→
**三条护栏同时变红**、各自指名自己的原因（① 每票 **5 / 6 / 6** 笔 + 拒单 **103**；
② 资金阶梯最大跳幅 **12.2023** pp + 高端残差 **7.3265** pp + 每档拒单 **754~1002**；
③ 矩阵的「只改资金」**3.4557** pp、「尺度不变」**1.4730** pp，各腿拒单 **964~1856**）；
还原后（`grep SABOTAGE` 零残留）三条回到绿。⚠️ 破坏态数字与「修复前完全未改」的
777~1005 略有差别，因为破坏只还原了**抵扣条件**这一处，`reconcileTargetPosition` 的
三处调用仍在 —— 两套数字都成立，别互相校对。2026-09-24，做 AUD-53 根因定位时**查出来的** |

| AUD-52 | 1 | 0 | 0 | 1 | 0 | ⬜ **AUD-52**（e2e 套件的**前置条件不再蕴含它的断言** —— `e2e/tests/integration_test.go` 的 `TestMain` 只检查「analysis 可达」，可达就整套跑；但 ① `executionURL := "http://localhost:8084"`（:248）指向**已退役的服务**（ODR-021 把 execution 并进 analysis，端点在 `:8085/api/execution/*`，容器里已无 :8084），**这个测试在当前架构下永远不可能通过**；② `TestStrategyAPI_ListStrategies` 打 `:8085/api/strategies` 期望 200，实测 **401** —— 鉴权上线后套件没有配套的取 token 步骤）。2026-09-23，**撞出来的**（完整跑测试时红，**与本轮改动无关**）。**同症状不同成因，故与 AUD-51 分开登记**：AUD-51 是前置条件**写错了对象**，AUD-52 是前置条件**不够** —— 套件级的门只保证「服务在」，不保证「服务要的东西你有」 |
| AUD-56 | 1 | 1 | 0 | 0 | 0 | ✅ **AUD-56**（**有一条测试要靠外网可达性才能结束**）**已修（2026-09-25）**。原状：`TestPipeline_ValidateCompilation_UsesBuildDir` 的立意是「证明 `buildDir` 真被用上」，做法是把 `buildDir` 指向一个没有 go.mod 的临时目录、再用 `import "github.com/nonexistent/fakepkg"` 逼那次 `go build` 失败；但解析这个 import **会联网** —— 网络可达时 10s 内通过，不可达时子进程一直等（全仓 `go test ./...` 时该包 **601s 被杀**，单跑 70s `panic: test timed out`）。**而它最后只断言 `p.buildDir == tmpDir`**（构造函数刚设过的值）—— 一句同义反复，**既没证明 buildDir 被用上，又把自己的成败交给了墙**。修法（两层，都不触网）：**① 测试改成正面证据 + 反证** —— `buildDir` 里放一个 `go.mod`，用 `replace example.com/localmod => ./localmod` 指向目录内的本地模块，被测代码 import 它；只有 `go build` 真的以 `buildDir` 为工作目录才会读到那条 replace、编译通过（**腿 1**），而同一个 `buildDir` 换成空目录必须失败（**腿 2 反证** —— 没有它，腿 1 可能是「无论在哪都成功」，测试又成摆设）；断言改读**退出码 / `BuildError` 是否为空**，不读错误文本格式；并 `t.Setenv("GOPROXY","off")` 把「不触网」变成**可断言的前提**而不是对网络状况的侥幸。**② 生产侧给子进程加上界** —— `validateCompilation` 改用 `exec.CommandContext` + `buildTimeout = 120s`：没有它，一个需要联网的 import 在生产侧表现为「**实验卡住**」而不是「编译失败」，而后者才是要写进 `result.BuildError` 给人看的 artifact（ADR-024）。**实测**：修后 6.18s 通过（原 601s 挂死）；`t.Setenv` 生效后即使把 `buildCmd.Dir` 破坏成 `""` 也是 **0.93s 变红**并指名原因，不再挂起。**破坏验证**：`buildCmd.Dir = ""` → 腿 1 变红且报出 `module lookup disabled by GOPROXY=off`；还原后回到绿，`grep SABOTAGE` 零残留。**取证教训（记在案）**：先做**单次** A/B（`git stash` → 通过；恢复 → 挂）看着像「本轮改动引起的回归」，改成 **A/B/A/B 交替四轮**（改动在 / 基线 / 改动在 / 基线）**全挂**才证明那次通过是网络侥幸 —— 判 flaky 必须多轮交替，不能一次定论。2026-09-24，跑全仓测试时**撞出来的** |

**原来的那处「阻塞」已解除，但换成了一个更大的问题。** P2-8 曾登记为「卡在
`TUSHARE_TOKEN` 未设置」，2026-09-23 token 到位、`daily` + `adj_factor` 实测可用，
前置确实解除了。**复核做完之后，结论不是「可以放心落 hfq 了」，而是「口径确实
实质性影响回测数字（21.6 个百分点），而这条差异的通道是引擎在**下单侧**的缺陷」**
—— 根因已定位到 `computeEffectiveTarget` 的抵扣条件（独立登记为 **AUD-55**），
它表现出来的「对价格水平 / 资金量级都不稳健」记为 **AUD-53**。
**⚠️ 2026-09-24 更新：AUD-55 已修**（抵扣改无条件 + 已持仓实时读 tracker），
资金侧的敏感度随之塌掉（4.82pp → 0.76pp、阶梯转收敛、零拒单）；剩下的价格侧差异
被确认为**绝对金额约束**（一手 = 100 × 价格，而资金固定），**不是引擎缺陷** ——
所以 P2-8 的 hfq 落库**换了一条判据**，不再是「等 AUD-55 修完」。详见本文件末尾
「P2-8 取证说明」。

**AUD 线**：AUD-01 ~ AUD-50 已全关；**AUD-53 / AUD-54 / AUD-55 于 2026-09-24 一次关掉**
（AUD-55 与 AUD-53 是「一个根因、两种表现」，两处一起改 + 三条回归护栏 + 破坏验证；
AUD-54 裁决走「逐条订正归档文档」，护栏一个字没动）。
**AUD-56 于 2026-09-25 关掉**（测试改成「正面证据 + 反证」并 `GOPROXY=off`，生产侧给 `go build`
子进程加了 120s 上界 —— 三层里那层「测试的前置条件依赖**外网**」已拆掉）。
**AUD-51 于 2026-09-25 关掉**（四个测试改为自灌自证 + **真库实测**在全新空库上全绿 + 破坏验证）。
**只剩 AUD-52 没动**（e2e 套件的前置条件**不再蕴含**它的断言：`:8084` 已退役、
`/api/strategies` 实测 401）。这一族三项都只影响**测试**的可信度，不影响回测数字。

**下一批建议**：**AUD-52**（三项测试可信度问题里唯一没动的；不修的话「跑红」这个信号
本身不可信）→ 然后 **P2-8 的 hfq 落库按新判据重新裁决**
（见文末「P2-8 取证说明」六）→ 然后 **P2-1**（补宏观数据源 —— 跨境那一半已做完，
见 `guides/data-dependencies.md`）与 **P2-2**（产业链数据底座）。

### 已完成（2026-09-16，已从下方列表移出）

- **P0-1 前视偏差**：财务读取改为按 `COALESCE(ann_date, trade_date)` 过滤
  （`GetFundamentalsSnapshot` / `GetFundamentals`）；回归测试见
  `pkg/storage/fundamentals_pit_test.go`（4 例，红→绿已验证）。
  顺带修掉两个扫描崩溃：`ann_date` 为 NULL 时报错、数值列为 NULL 时报错。
- **P0-3 放开测试**：`.gitignore` 的 `*_test.go` 规则已移除，新测试可正常入库。
  副作用：立刻暴露出此前被隐藏的 `cmd/analysis/handlers_proxy_test.go`。
- **P0-2 建 CI**：`.github/workflows/ci.yml`，四个步骤 —— `go build` / `go vet` /
  `go test ./...`（带 Postgres 服务容器，否则存储层会 skip）/ 文档链接校验。
  顺带修掉全仓基线上的 4 个失败，以及一个真 bug：
  `generateOrderID()` 仅用 `UnixNano()`，Windows 时钟粒度粗导致**连续订单 ID 相撞，
  后一笔静默覆盖前一笔** → 加 atomic 序号（`pkg/live/order_manager.go:258`）。
- **P0-4 鉴权收口**：① 无密钥直接 `Fatal` 拒绝启动（裁决逻辑拆成纯函数
  `decideAuthStartup` 以便测试）；唯一豁免是 `AUTH_INSECURE=true` **且**
  `server.host` 为 loopback —— 非 loopback 时豁免无效。
  ② CORS 从硬编码 `*` 改为按 `server.cors.allowed_origins` 回显，留空即 fail closed；
  analysis / data 两份复制粘贴的中间件收成 `internal/httpserver.CORS`。
  回归测试：`cmd/analysis/auth_bootstrap_test.go`、`internal/httpserver/cors_test.go`，
  两侧各一个装配测试（防"实现改对了但 buildRouter 没接上"）。
  **副作用**：`docker-compose.yml` 的 analysis 服务现在强制要求 `JWT_SECRET`
  （未设则 compose 报错退出）；本地 `go run ./cmd/analysis` 也要带密钥或走豁免，
  见 `guides/local-dev.md`。
- **P0-5 实验链路打通**（根因比原描述深两层，见 [ADR-024](adr/adr-024-expression-as-execution-target.md)）：
  ① `Execute` 在生成 YAML 后**从未把策略注册进 registry**，Stage 5 拿
  `parsedIntent.StrategyName` 回测必然 `strategy not found` —— 补上 `buildAndRegister`；
  ② 更深一层：YAML 生成器只在 intent 自带 `signal_expr` 时才产出 expression 段，
  规则解析出的 intent 只有语义类型（momentum/…），`LoadStrategy` 只认 expression
  → 生成的 YAML 根本加载不成策略。补 `defaultSignalExpression()`：意图类型 →
  确定性默认表达式（价量可表达者）；value / quality 需要 PE/ROE 而表达式引擎
  只有 OHLCV，**明确失败而不是给假数字**。
  ③ LLM 生成的 Go 代码降级为**可审阅 artifact**：编译失败 / LLM 未配置只记录不阻断。
  ④ `Execute` 与 `ExecuteAsync` 的五段逻辑是复制粘贴的两份 → 合并为 `p.run`，
  只修一边就会漏另一边。
- **P0-6 编译校验恒真**：`go tool compile -V=full` 是**打印编译器版本号**的开关，
  根本不读文件，`Compiles` 恒为 true。换成在 module 根下真跑 `go build`；
  找不到 go.mod 时报「无法验证」而非假装通过（fail closed）。

---

## P0 — 正确性（不修，其它一切都是沙上建塔）

**P0-1 ~ P0-6 已于 2026-09-17 全部完成**（明细见上方「已完成」）。
S0 止血阶段的出口判据已满足，见 [ROADMAP](ROADMAP.md)。

**2026-09-21 全栈审查（[ODR-065](archive/odr/odr-065-fullstack-static-review.md)）新增 5 项 Critical** — 证据行号、代码示意与 15 commits 修复方案见 [审查报告 §4/§16](archive/reports-2026-Q3/review-report-20260921.md)。每任务一个原子 commit，测试先行。

**已完成 5 项**：AUD-03（C1 日收益率）、AUD-04（C2 窗口隔离）、AUD-05（C5 DDL 断层）、AUD-01（C3 save 端点 —— **裁决为删除而非加固**：零生产调用方 + 用途与 ADR-024 冲突，攻击面归零优于加固后仍存在。护栏 `cmd/analysis/handlers_copilot_test.go` 断言该路由返回 404，已故意加回端点验证过它会变红）、AUD-02（H5 RBAC 接线）。

> 原「前置：推送 main 领先 origin 的 51 提交」（P2 区 AUD-L1）经复核为**误报**，已作废 —— 实测 `git rev-parse main` == `git ls-remote origin refs/heads/main`。

### AUD-02 落地说明（2026-09-21）

三项决策由若曦裁定：① **纳入 paper**；② legacy 根路径**一并挂同角色**；③ 豁免模式用**服务层短路**。

- **`pkg/auth` 新增 `Service.RequireRole`**（非包级 `RequireRole`）。根因：`Middleware()` 在 `!Enabled()` 时是纯 no-op、**不设 CtxRole**，包级 `RequireRole` 跟在它后面会让豁免模式下**所有请求 401**。规矩写进 doc comment：凡由 `s.Middleware()` 保护的分组，必须配 `s.RequireRole(...)`，两个决定（auth 是否开、要什么角色）必须一致，且只有 Service 知道第一个答案。
- **`pkg/tools/sideeffect.go`（新）**：21 个工具的副作用登记表（19 读 / 2 写）。选集中表而非给 `ToolCore` 加方法 —— 加方法会在编译期打断所有现有实现，且分类散落到各工具里更容易写不一致。**fail-closed**：未登记 = admin。两条漂移测试钉住「登记表 == setup.go 实际注册集」，双向都查（漏登记 / 陈旧行各一条）。
- **`/api/tools/:name` 的角色判定在 handler 内**：路由是通配路径，per-route 中间件表达不了「按工具决定角色」。检查在**读 body 之前**做 —— 无权调用的工具，不该让调用方能区分「body 错」和「你没权限」。
- **`/emergency-flatten` 不加 RBAC**（有意）：它已有服务端配置的 bearer token + confirmation_token 双因子；再加 JWT 依赖会在「auth 服务本身挂了」时锁死平仓路径，而那正是最需要它的时刻。
- **护栏经三处故意破坏实证**（记忆里的铁律：假护栏比没护栏更糟）：
  1. 拆掉 legacy 根路径的 `requireTrader()` → 只 `TestExecution_LegacyPath_SameAuthorityAsAPIPath` 变红，其余 10 项仍绿；
  2. 把 `s.RequireRole` 换回包级 `RequireRole` → `pkg/auth` 与 `cmd/analysis` **两层**同时变红；
  3. 把 `Classify` 的兜底改成 `SideEffectRead`（fail-open）→ `TestClassify_FailClosed` + `TestTools_UnclassifiedTool_FailsClosed` 同时变红。
- **顺带发现（新登记 AUD-19）**：`/api/paper/*` 全部 10 个端点**从未挂载** —— `registerPaperTradingRoutes` **零调用方**。原本担心它是「第二处无鉴权写端点」，实为「根本没挂上」。**给不可达代码加 RBAC 是纯装饰**，故不在 AUD-02 内处理，改走 AUD-19（裁决：删除）。
> ⚠️ **本条当时的措辞「全仓无前端/文档引用」是错的**（2026-09-22 在 AUD-19 里核实）：前端有完整的「模拟交易」页面在调这 10 个路径，`docs/SPEC.md` 也有一整节在描述它们。真正的结论要更强：不是「没人用」，而是「**有人在用，但后端从未挂上 → 那个页面打开就是 404**」。结论（删除）没变，理由变了 —— 见 AUD-19 落地说明。

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|

---

## P1 — 地基与回路

| # | 任务 | 位置 | 验收 |
|---|---|---|---|
| **P1-1** | **实验日志**：记录 AI 每次尝试（参数向量 / 结果 / 父子关系 / 假设来源） | **✅ 2026-09-17**（子项 1a/1b/1c 均已完成）`experiments` 表 + `pkg/storage/experiments.go` | 能回放一条完整探索路径 |
| **P1-1a** | └ 存储层：表 + 存取（插入 / 收尾 / 单查 / 按 seq 回放） | **✅ 2026-09-17** | 单测 5 例全绿，表已落库 |
| **P1-1b** | └ 生产者：pipeline 每次尝试落一行，**失败也要落**；`cmd/analysis` 启动时注入 sink。⚠️ `UNIQUE(run_id, seq)` 要求 seq 由调用方分配 —— 并发跑时不能让两边各自 +1 | **✅ 2026-09-17** `pkg/ai/pipeline/pipeline.go`、`cmd/analysis/handlers_pipeline.go:46` | 真库端到端取证通过（跑完落 completed，失败落 failed） |
| **P1-1c** | └ 回放：路径可读。单次「试了什么」+ 多步「为什么转到下一步」都已具备（expression / params / hypothesis / 指标 / parent_id） | **✅ 2026-09-17** | 一条 run 从 seq 0 到 seq n 能讲成一个故事 |
| **P1-2** | **循环控制器**：让搜索算法（或 LLM）能连续调用底座 + 响应中断 | **✅ 2026-09-17** `pkg/ai/loop/`（新包；TPE 已复用，遗传仍零调用） | 连续跑 N 次 ✅、中途叫停 ✅、单次失败不中断 ✅、seq 连续 + 父子链 ✅ |
| **P1-2b** | ~~搜了但没生效~~ → **已修**：`defaultSignalExpression` 改为读意图参数 + 开结构化传参通道 `WithParameterOverrides`。⚠️ **搜索空间的参数名必须与意图参数同名**（`lookback_days`）—— 写成 `lookback` 之类别名**不报错、只是静默失效**，整轮退化成同一个策略跑 N 遍 | **✅ 2026-09-17** `pkg/ai/yaml/generator.go`、`pkg/ai/pipeline/pipeline.go`、`pkg/ai/loop/loop.go` | 取证：一轮 6 次窗口各不相同（15/30/33/22/47/35）；另有不依赖库的回归 `TestRun_DifferentParamsProduceDifferentConfig` |
| **P1-3** | **观察页**：三栏（正在试什么 / 结果流 / 当前最优）+ 干预入口 | **✅ 2026-09-17** `web/src/pages/Explore.vue`、`web/src/api/explore.ts`、`cmd/analysis/handlers_explore.go` | 能看见路径形状，能输入方向，能叫停 |
| **P1-4** | ~~**schema 收口**：真实 DDL 硬编码在 Go 里，`migrations/` 无版本管理。**含语义债**：`stock_fundamentals.trade_date` 被两种写入路径复用~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/data/tushare.go` + `pkg/storage/fundamentals.go` + `postgres.go`（migration 031） | **调查推翻了原描述**：不是"两种数据源语义不同"，两条路径调的是**同一个 `fina_indicator`**、**都把 end_date 写进 trade_date**；真正的差别是路径 A（`normalizeFundamentals`）把 API 已返回的 `ann_date` **直接丢弃**。所以 P0-1 的 `COALESCE(ann_date, trade_date)` 只是把洞盖住 —— 对路径 A 的行它退化成报告期截止日，三季报（9/30 截止、10/25 披露）仍在 9/30 可见，前视偏差没清。修法：① 路径 A 补存 `ann_date`；② 新增 `available_date` **生成列** `GENERATED ALWAYS AS (COALESCE(ann_date, trade_date)) STORED`，10 处读取全部改用它（此前 5 处 COALESCE、5 处裸用 trade_date）。**为什么用生成列而不是普通列**：普通列要靠每个写入函数记得填，任何绕过写入函数的路径（手工 INSERT、修数）都会留下 NULL，那行数据随后从所有按可用日过滤的查询里凭空消失。生成列从物理上保证它不可能为空、也不可能和 COALESCE 不一致。另修零值脏数据：`FundamentalData.AnnDate` 是 `time.Time` 非指针，缺字段时写入 0001-01-01 会让 COALESCE 拿到"公元前"，整行不可见 —— 写入侧用 `nullableDate` 转 NULL，存量用迁移清理。**schema 收口**：不做迁移（64 条 Go DDL 迁到 golang-migrate 的收益是洁癖，风险是回测依赖的表结构漂移），改为删除零调用的 `MigrationManager` + 在 `postgres.go` 写明「DDL 唯一真相在此，末尾追加并带 `// Migration 0NN:` 注释」；`migrations/` 与 `docs/migrations/` 留作历史记录，只读不执行 |
| **P1-5** | ~~**统一错误中间件**：154 处手写 `gin.H{"error"}`，`c.Error()` 使用 0 次~~ | **✅ 2026-09-18** `internal/httpserver/errors.go` + `cmd/**`（29 个文件、317 处） | 统一出口 `Fail` / `Failf` / `Error` / `Wrap` / `FailCause` + 兜底中间件 `ErrorMiddleware`（只接住「调了 c.Error() 却没写响应」的，已写响应的不覆盖）。**真正的收益不是整洁，是关掉泄露**：此前 5xx 直接把 `err.Error()` 吐给客户端（DB 报错 / 连接串 / 内部路径），现在 5xx 一律通用文案、原因只进日志，4xx 保留原文（那本来就是给人看的）。响应体保持 `{"error": ...}`（前端读这个字段）并新增 `code`。批量转换脚本留在 `tools/convert_error_responses.py`（它只转单行单键形态，跨行/多键一律跳过）。**剩余 4 处**是 `c.SSEvent("error", ...)` —— SSE 是另一条通道（响应已是 200 + 流），不属此列 |
| **P1-6** | ~~**无界 goroutine**：信号量只限并发执行，goroutine 数 = 任务数~~ | **✅ 2026-09-18** `pkg/backtest/workers/`（新 leaf 包）+ `batch/batch.go` + `walkforward/walkforward.go` | 新增泛型 `workers.RunWorkers(ctx, n, jobs, fn)`：先起固定 N 个 worker 从 channel 领活，goroutine 总数 = min(N, 任务数)，与任务数无关。**修之前的问题**：`for { go f() }` + 信号量只限制了同时**执行**几个，1 万个任务的 goroutine 已全部建出，内存/调度是 O(任务数)。两点约定写进文档：① 结果按输入下标落位（完成顺序不定但落位确定，守住 P1-14 可复现）；② ctx 取消后不再领新活（在跑的不强杀）。leaf 包是为避免循环依赖 —— walkforward 不能 import backtest，contracts 是 DTO/接口包不放执行器 |
| **P1-7** | ~~**N+1 查询**：300 只票 = 600 次 DB 往返~~ | **✅ 2026-09-18** `pkg/storage/ohlcv.go`（新增 `GetClosesOn`）+ `pkg/data/factor_attribution.go` | 因子归因原本对每只票调两次 `GetOHLCV`（当前日 + 远期日），300 只票 = 600 次往返。改为每个日期**一次** `WHERE symbol = ANY($1) AND trade_date = $2`。**没行情的票不出现在返回 map 里** —— 调用方据此判断缺失，塞 0 会被读成"白送的股票"（P2-10 那类坑）。测试用计数假 store 断言 `closesCalls == 2` 且 `ohlcvCalls == 0`：**只验结果测不出 N+1**，必须数查询次数 |
| **P1-8** | ~~**部署编排三份合一**：compose × 2 + k8s × 8，互相漂移~~ | **✅ 2026-09-18** `tools/check_deploy_consistency.py`（新）+ `docs/guides/deploy-config.md`（新）+ `deploy/k8s/configmap.yaml` + `.github/workflows/ci.yml` | 现状先变了：compose 已收敛到 1 份（P1-10 删的），k8s 剩 6 份（P2-5 删了 ai-deployment）。**没做「一份源生成两份」** —— 那要引入 Kompose/Helm，为几处配置给单人自托管项目加一整套生成链，收益不抵复杂度。改为**护栏 + 文档**：① 新增一致性校验脚本（比对服务端口与 `DATA_SERVICE_URL`，CI 里跑，实测能把人为改错的两处都抓出来）；② 写清「改端口要动 4 个地方」。**顺手修两处死配置**：configmap 的 `ANALYSIS_PORT`/`DATA_PORT` 从未被读取（代码读 viper 的 `server.port`），已删；`DATA_SERVICE_URL` 在 k8s 里长期缺失、全靠代码默认值碰巧能用，已显式配上 |
| **P1-9** | ~~前端 `/alerts` `/compliance` 缺 `/api` 前缀，dev 下必 404~~ | **✅ 2026-09-18** `web/src/api/`（alerts / compliance / paper-trading / market） | 实际比登记的多：除了 alerts、compliance，paper-trading 有 9 处、market 的 `/health` 也没前缀 —— dev 下 vite 只代理 `/api`，其余一律 404。顺带发现 **`/api/health` 后端根本没注册**，而 Dockerfile 的 HEALTHCHECK 打的就是它 —— 容器从启动起就被判 unhealthy，现在补了别名（与 `/health` 共用 handler） |
| **P1-10** | ~~**`make build` 会失败**：Makefile 仍 build `cmd/execution`、`cmd/risk`，这两个目录 ODR-021 合并后已不存在~~ | **✅ 2026-09-18** `Makefile` | 删掉两个死目标、加 `build-ai` / `push-ai`；`up/down/restart/logs/ps` 从 `docker-compose.services.yml` 改指根 `docker-compose.yml`（前者引用了已删服务的 Dockerfile，一读就炸；docs/guides/local-dev.md 早已记下「只用 docker-compose.yml」，这次把 Makefile 对齐到那个决定，并删掉漂移的那份） |
| **P1-11** | ~~**`cmd/ai` 无 Dockerfile、不在 `docker-compose.yml`**，只能本地 `go run`~~ | **✅ 2026-09-18** `cmd/ai/Dockerfile` | 补 Dockerfile（:8086，探针是 `/health` 不是 `/api/health`，AI 密钥走环境变量不入镜像）。**未**加进 compose —— 根 compose 明确写了 ai 是单独起的（`cmd/ai` 走的是 DEPRECATED 的 `pkg/ai/agents`，见 P2-5，把它塞进默认编排反而是固化一条待废弃路径） |
| **P1-13** | **稳健校验器把「稳定地不赚钱」判成稳健**：「邻域站得住」的阈值取中心的一半，中心 Sharpe 趋零时门槛也趋零 → 中心 0.031 也能拿到高原面积 1.00、概率 1.000。**已修（2026-09-17，随 cf2bbaa）**：按中心高度打折（`MinMeaningfulSharpe=0.5`），同一组数据给 0.248，并提示「别把稳定地不赚钱当成稳健」。单测发现不了 —— 手写的邻域数字都是「合理」的 | **✅** `pkg/validation/robustness.go` | 平坦性说的是结论稳不稳，有效性说的是值不值得做，两者不能互相替代 |
| ~~**P1-14**~~ | ~~**回测不可复现**~~ → **已修（2026-09-17）**：同一份数据连跑两次，成交 122 vs 120 笔、收益 1.33 vs 1.31。四处非确定性来源：① momentum 遍历 `bars` map + `sort.Slice` 不稳定 → top-N 选谁每次不同（8 只动量相同的票选 3 只，200 次调用出 8 种组合）；② tracker 持仓求和顺序（浮点加法不满足结合律，差 1e-10，复利放大后变 38 元）；③ 止损 / 强平遍历持仓 map → 同日平仓顺序互换；④ regime 检测拼接行情顺序 → 仓位倍数不同。**现已 684 笔成交逐位一致** | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go`（regime 拼接 + 止损定序）、`pkg/backtest/engine_daily.go`（强平定序）、`pkg/strategy/examples/momentum.go` | 回归测试：`pkg/backtest/engine_reproducibility_test.go`（跑两次逐位比对 + top-N 顺序无关性） |
| ~~**P1-12**~~ | ~~**回测的「今天」取自 `time.Now()`**~~ → **已修（2026-09-17）**：两层都改了。① 引擎侧：`Tracker` 新增 `asOf`，主循环在生成信号**之前**调 `SetAsOf(date)`，`GetPortfolio` 用它填 `UpdatedAt`（零值才退回墙钟 —— 那是实时撮合场景）。② 策略侧：momentum 不再用 `time.Now()` 兜底，改为「组合快照 → 行情最新日期 → 直接报错」；`multi_factor` / `value_screen` 无日期时不发信号；`convertible_bond` 的纯债折现改用回放日期。取证：weekly 252 天 0 成交 → 114 笔 | **✅** `pkg/backtest/tracker/tracker.go`、`pkg/backtest/engine.go:625`、`pkg/strategy/utils.go`（新增 `LatestBarDate`）、`examples/momentum.go`、`plugins/{multi_factor,value_screen,convertible_bond}.go` | 回归测试：`pkg/backtest/engine_rebalance_date_test.go`（spy 盯契约 + weekly 盯后果 + tracker 单测） |

**2026-09-21 全栈审查（ODR-065）新增 8 项 High** — 证据行号与修复代码示意见[审查报告 §5/§16](archive/reports-2026-Q3/review-report-20260921.md)：

**已完成 8 项**：AUD-06（印花税）、AUD-07（涨跌停板块分档 + 分取整）、
AUD-08（`*ST` 识别，与 AUD-07 同一 commit 合入）、AUD-09（整手归一）、
AUD-10（MockTrader 读路径写穿共享对象）、AUD-11（沙箱资源限制真正作用在子进程）、
AUD-12（CI 补 `-race` 门禁 + frontend job）、AUD-13（compose PG/Redis 端口绑回环）。

> **AUD-06 落地说明（2026-09-21）**：`DefaultStampTaxRate` 由 `0.001` 改为 `0.0005`，
> 沿革为 **2023-08-28 起 0.1% 减半至 0.05%**（财政部/税务总局 2023 年第 39 号公告），
> 原注释写的「0.2% → 0.1%」两头都错 2 倍、方向一致，所以错误自洽难发现。
> **波及面比登记的大**：除 `pkg/fees/ashare.go`，还必须在
> ① `config/analysis-service.yaml`（`trading.stamp_tax_rate: 0.001` —— 这个值**覆盖**常量默认值，
> 不改则等于没改）、② `docs/ARCHITECTURE.md` 费率示例、③ 三个固化旧值的测试
> （`pkg/fees/ashare_test.go`、`pkg/portfolio/portfolio_test.go` 的 `26.2`、
> `cmd/analysis/handlers_risk_execution_test.go` 的 fixture）同步修正。
> 护栏三处实证：改回 `0.001` → 三条测试变红且报出金额（100 vs 50）；改成 `0.00025` →
> 日期沿革测试也报红。
> **顺带发现（新登记 AUD-20）**：`Tracker.feeSchedule()` 返回**固定**费率，不看
> `ExecuteTrade` 已有的 `timestamp` —— 回测窗口若横跨 2023-08-28，卖出费率全程用同一个值。
> 这是加功能而非修 bug，单独决策。

> **AUD-07 + AUD-08 落地说明（2026-09-21，同一 commit）**：两项共享同一段
> `engine_daily.go` 的 if-else，故合并实施 —— 先抽纯函数，AUD-08 复用。
>
> **AUD-07**：新增 `pkg/backtest/pricelimit.go`，把内联分支抽成纯函数
> `resolvePriceLimit(in, cfg)`，优先级 **新股 > ST（且 ST 受板块约束）> 板块**。
> 上限/下限价经 `LimitPrices()` 做 `math.Round(x*100)/100`（分取整）后再比较。
> 关键设计：**ST 不无条件用 ST 费率** —— 创业板/科创板（注册制）的风险警示股
> 保持板块 ±20%，北交所保持 ±30%，只有主板 ST 走 5%/10% 档。
> naive 的 `if isST { return stRate }` 会让创业板 ST 错用 5%。
>
> **审计报告漏掉的两处（本次新发现）**：
> ① **主板 ST 已于 2026-07-06 从 ±5% 上调至 ±10%**（沪深北三所 2026-04 修订
> 交易规则），故新增 `st_before` 配置 + `STBefore` 常量，按 `AsOf` 日期分段；
> 创业板/科创板 ST 不受影响。② **`marketdata.ClassifySymbol` 不认 `301`
> （创业板注册制新增段）和 `689`（科创板 CDR）** → 这两类票的涨跌停被当成
> 主板 10%，而 `pkg/live/price_cage.go` 同样受影响。已在 `pkg/marketdata/board.go`
> 根因处修复（`301xxx`/`689xxx` → 对应板块），而非在调用点打补丁。
>
> **AUD-08**：`hasSTPrefix` 原来的 `name[:2] == "ST"` **永远匹配不到 3 字符的
> `*ST`**（`name[:2]` 是 `"*S"`）。改为前缀匹配四种模式（`*ST`/`S*ST`/`SST`/`ST`）。
> 测试 `TestEngine_hasSTPrefix` 原先把 bug 写成了规格（`{"*STXYZ.SH", false}`），
> 同步修正为真实股票名并补 `SST`/`S*ST`/空串/带空格用例 —— **故与实现同一 commit**。
>
> **配置覆盖陷阱（同 AUD-06）**：`config/analysis-service.yaml` 的
> `price_limit.st` / `price_limit.new` 会覆盖 Go 常量，已同步改 `st: 0.10`、
> 增 `st_before: 0.05`。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① ST 判定退回 `name[:2]` → `TestEngine_hasSTPrefix` + `TestIsRiskWarningName` 精确变红；
> ② `301`/`689` 退回 `BoardUnknown` → `pkg/marketdata` 与 `pkg/backtest` **两层**同时变红；
> ③ ST 分支忽略板块（naive `if isST`）→ 4 个注册制 ST 用例变红（0.2 vs 0.1）；
> ④ `roundToCent` 改 `math.Trunc` → 验收项 **10.05→11.06** 报出 `11.05`；
> ⑤ 日期分段失效 → 两个 pre-change 用例变红（0.05 vs 0.10）。
>
> **过程中的一个方法学坑**：第 ③ 处破坏最初写成 `case A, B:` + 空 body，
> 以为是 fall-through，**Go 的 case 并不 fall through** —— 空 body 直接退出 switch，
> 等于没改，测试自然不红。**「护栏没变红」要先怀疑破坏本身无效，再怀疑护栏**。
> （另有一次破坏写成 `return price` 导致 `math` 未使用**编译失败** ——
> build failure 不是 test failure，不能算护栏生效。）
>
> **已知偏差（用户裁定保留，本次不修）**：新股档 `new_stock_days: 60` 不准 ——
> 真实规则是「上市首日 + 前 5 个交易日无涨跌幅限制，其后按板块」，与「60 个
> 交易日内一律 ±20%」不同。已在 `pricelimit.go` 的 `PriceLimitInput.TradeDays`
> 注释里明确写出「这是历史（不正确）行为」。同批修复会让 AUD-07 过大，故单列。
>
> **`PriceLimitInput.TradeDays` 零值语义**：零值 = 「今天上市」（必然 < 阈值），
> 没有 unset 哨兵值。写测试时必须显式传成熟交易日数（`matureTradeDays`），
> 否则所有用例都会被 New 分支吞掉 —— 这个坑在开发时真实踩到过。

> **AUD-09 落地说明（2026-09-21）**：勘察证实**登记写的验收标准本身是错的**。
>
> **现状（正向确认，不是负向证据）**：`Weight→shares` 换算在
> `pkg/risk/manager.go`（单个 + 批量）与 `pkg/risk/volatility.go` 共三处，
> 全部是 `math.Floor(positionValue / price)` —— 只取整到**整股**，
> 不认整手。实盘侧 `pkg/live/broker/xtp/xtp.go` 反而已有约束
> （`int(quantity)%100 != 0` → 报错），**回测与实盘不一致**：回测算出的
> 5013 股在实盘会被券商拒绝。这是「两个机制各自对、接起来就错」的第五例。
>
> **登记标准「下单量恒为 100 倍数」对科创板/北交所是错的**（已查官方原文）：
>
> | 板块 | 买入申报数量 | 出处 |
> |------|-------------|------|
> | 沪/深主板、创业板 | **100 股或其整数倍** | 上交所/深交所《交易规则（2023 修订）》3.3.8 |
> | 科创板 | **≥200 股**，1 股递增 | 上交所《交易规则（2023 修订）》6.1.7 |
> | 北交所 | **≥100 股** | 北交所《交易规则》3.3.8（2026-07-06 施行） |
>
> 关键：**只有主板/创业板是 bug**。科创/北交所当前 `math.Floor` 的结果
> （如 5013 股）**恰好合法**（≥下限且允许 1 股递增）；强行统一成 100 倍数
> 反而会给它们引入系统性少买偏差（5013 → 5000）。
>
> **顺带查证并否证**：2023-08 讨论的「100+1」（主板/创业板改为
> 100 股起、1 股递增）**从未落地** —— 当时只是「拟调整 / 研究」，
> 现行 2023 年修订版仍是「100 股整数倍」。按「100+1 已生效」写的代码是错的。
>
> **实现**：新增 `pkg/risk/lot.go`，`NormalizeOrderQuantity(shares, symbol)`
> 按 `marketdata.ClassifySymbol` 分档；三处 sizer 改为调用它（`math` import
> 从 `manager.go` 移除 —— 它只有这两个用途）。常量 `LotSize=100` /
> `STARMinShares=200` / `BSEMinShares=100` 是**监管硬规则，不进配置层**
> （已 grep 确认 `risk_manager.*` 无同名覆盖项；operator 改了会产生非法订单）。
> `BoardUnknown` 回落到主板规则 —— 三种规则里最严，误分类也仍合法。
>
> **不足下限时提到下限**（用户裁定）：50 股 → 100 股，而非跳过。
> **代价已知**：会放大订单，最坏情况（输入 1 股）达 100 倍；资金不足时
> `Tracker` 的 `insufficient cash` 会兜底报错。另设一条有意边界：
> **输入 < 1 股归 0**（`positionValue/price` 很小时会产生 0.4 股这种商，
> 提到 100 股等于凭空放大 250 倍 —— 「不足一手」是仓位决策，
> 「不足一股」是错误）。
>
> **顺带修正 AUD-07 的遗漏**：`pkg/marketdata/board.go` 包注释仍写着
> 「ST 股票 ±5%; *ST 股票 ±5%」—— 对注册制板块是错的（应为保持板块档
> ±20%/±30%），已改为分板块说明并指向 `pricelimit.go`。
>
> **测试缺口（勘察时发现）**：现有测试只断言 `ps.Size > 0` / `>= 0`，
> **从不检查具体数值**，所以改动不会被发现。新增 `pkg/risk/lot_test.go`：
> 逐例表驱动 + **规格级性质断言**（输出恒满足板块规则）+ 端到端接线测试
> （构造 `617.28` 这个非整手商，使主板得 600、科创/北交所得 617 ——
> 三者互不相同，能同时证明「分档生效」与「归一被调用」）。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① 忽略板块统一 floor → 主板端到端红（600 vs 617）；
> ② 科创板也用 100 倍数 → 只红科创用例（5013 vs 5000）；
> ③ 提到下限改归 0 → 提到下限用例 + 现有 `CalculatePositionsBatch_Basic` 红；
> ④ `shares < 1` 改 `<= 0` → 断崖与 sub-share 用例红；
> ⑤ sizer 不调用归一（改回 `math.Floor`）→ 端到端红（接线本身被验证）。
> 过程中一处破坏写成 `_ = symbol` 导致 `marketdata` 未使用 → **编译失败**，
> 按既有规矩不计入（build failure ≠ test failure），已改成行为有效的破坏。

> **AUD-10 落地说明（2026-09-21）**：`GetPositions` / `GetAccount` 在 `RLock` 下
> 经 `range` 拿到的 `*PositionInfo` **写回了刷新价**。`positions` 是
> `map[string]*PositionInfo`，range 变量是**共享对象的指针**，读锁下写它
> 既是数据竞争，也把「谁最后读谁说了算」的临时价格**固化**进了持仓记录。
>
> **并发场景是真实的、不是理论风险**：`cmd/analysis/alert_loop.go` 的后台
> 周期任务调 `GetAccount`，而 `handlers_execution.go` 的 HTTP handler 同时读
> —— 两个 reader 打同一个字段。
> （原文还列了 `handlers_paper_trading.go`；该文件已随 AUD-19 删除，且它当时**根本没挂载**，所以那次列举里其实只有 execution 一侧是真的。）
>
> **实现**：新增 `snapshot(pos) PositionInfo` helper，先 `cp := *pos` 再在副本上
> 取价；两处调用点改为在副本上算 `MarketValue` / `UnrealizedPnL`。
> `PositionInfo` 是**扁平值类型**（无指针/切片/map），故结构体拷贝即深拷贝。
> `GetOrder` 早已是这个写法（`copy := *order`），本项是把它补齐到另两处。
>
> **已核实不会破坏紧急平仓**：`flattenPosition` 用 `pos.CurrentPrice` 只是
> **feed 挂掉时的回落价** —— 它在第 464-468 行自己调 `PriceProvider`，
> 只有取到 `<= 0` 才用 stored price。修复不动这条路径。
>
> **护栏四处实证**（每处故意破坏 → 确认变红 → 恢复）：
> ① `GetPositions` 改回写 `pos.` → 红 2 例（`GetPositions_DoesNotWriteThrough`
> + `ConcurrentReads`），**`GetAccount` 用例保持绿**；
> ② `GetAccount` 同理 → 红 2 例，**`GetPositions` 保持绿**；
> ③ 把 bug 塞进 `snapshot` helper 内部（未来重构最可能重新引入的位置）→ 红 3 例；
> ④ 删掉 `snapshot` 里的取价 → 红 5 例，而 `NoPriceProviderLeavesStoredValuesAlone`
> 与 `ReadsAreRepeatable` **保持绿**（本就不依赖取价）。
> ①② 的「只红该红的」是这组护栏有区分度的证据。
>
> **测试无法依赖 `-race`**：本机无 gcc，`-race` 跑不起来。故断言
> **直接检查 `mt.positions[...]` 的存储态**（同包可见），不靠竞争检测器；
> `TestMockTrader_ConcurrentReads` 作为第二道防线，等 AUD-12 的 CI `-race` 生效。
>
> **过程中的两个坑（都是「零值不是没填」的变体）**：
> ① `MockTraderConfig` 用 `<= 0` 判「未设置」，测试传 `SlippageRate: 0`
> 被替换成 `pkg/fees` 默认值 `0.0001`，成交价不是 `10.0` 而是 `10.0001`；
> ② `SubmitOrder` 对**市价单**用 `PriceProvider` 定价（忽略传入 price），
> 而测试恰恰要构造「provider 价 ≠ 成交价」才能区分「已刷新」与「未刷新」。
> 解法：改用**限价单**建仓，并从 `mt.positions[sym].CurrentPrice` **读回**真实
> 成交价（不假设），再用 `math.Abs(fillPrice - marketPrice) > 1e-6` 做前置断言。
>
> **顺带发现（新登记 AUD-23）**：`pkg/live/engine.go:173-176`
> `func (e *LiveEngine) GetPortfolio() *domain.Portfolio { return e.portfolio }`
> —— 返回内部指针且**完全不加锁**，调用方可直接改写引擎状态。与 AUD-10 同族
> （读访问器暴露/改写共享状态），但对象不同（引擎组合 vs 模拟盘持仓），单列。

> **AUD-11 落地说明（2026-09-21）**：登记只说「Windows 无 rlimit 能力时 fail-closed」，
> 勘察发现**真正危险的是 POSIX 侧** —— 而且审计报告 H7 的「生产 Linux 不受影响」
> 这句判断是错的。
>
> **两个缺陷，同一处代码**：
>
> ① **Windows 静默 no-op**（登记项，属实）：`rlimit_windows.go` 的 `applyLimits`
> 无条件 `return nil`，生产传的 `1 GiB / 25 CPU 秒 / 256 fd` **一个都没生效**，
> 日志里也没有任何痕迹。这直接违背 ODR-020 自己写下的「不会静默忽略」设计目标。
>
> ② **POSIX 把限制打在守护进程身上**（本次新发现，Critical）：`rlimit_posix.go`
> 的 `applyLimits` 最终调 `applyLimitsPreExec(l)` → `syscall.Setrlimit(...)`，
> 而 `Run()` 里**并没有** `syscall.Exec`（注释写「then call syscall.Exec to
> replace ourselves」，代码里根本没这回事）。于是限制落在**调用者自己**身上，
> 而调用者就是 analysis-service 守护进程：
>
> | 生产值 | 对守护进程的实际后果 |
> |--------|---------------------|
> | `CPUSeconds: 25` | 累计 25 秒 CPU 后 **SIGXCPU 杀死守护进程**（Go 运行时不为 SIGXCPU 装 handler，默认动作即终止） |
> | `MemoryBytes: 1<<30` | 守护进程地址空间被限 1 GiB，跑回测时极可能 OOM |
> | `OpenFiles: 256` | 一个 HTTP 服务器被限 256 个 fd |
>
> 子进程只是**顺带继承**了这些限制。ODR-020 的作者明确写了「setrlimit 在父进程
> 调用」，但只承认了「并发 Run() 会互相污染」，**没有意识到被限的是守护进程本身**。
>
> **为什么从没被发现**：全仓**没有任何测试传非零 limits** 走过
> `applyLimitsPreExec` —— 这条路只在生产第一次 `go build` 时才第一次执行。
> 又一次「测试全绿 = 没有测试」。
>
> **修复**：`applyLimits`/`applyLimitsPreExec` 删除，改为 `prepareArgv` 在
> **命令行层面**把限制交给子进程：`sh -c '<ulimit …>; exec "$0" "$@"'`。
> shell 给自己设限后 exec 目标，限制恰好落在目标进程上，父进程毫发无损。
> 每个 `ulimit` 都带 `|| { echo 'runner: cannot set …' >&2; exit 125; }` ——
> 设不上就**中止**，绝不留下一个「以为有限制、实际没有」的子进程。
>
> - **零 limits 时不做包装**：不付 shell 开销，也不要求 PATH 里有 `sh`。
> - **参数走 `$0`/`$@` 而非字符串拼接**：shell 没有第二次解析机会
>   （含空格/通配符/`$` 的参数原样抵达，已实测）。
> - **`ulimit -v` 单位是 KiB、`-f` 是 512 字节块，且 0 = 不限** →
>   字节数**向上取整**，1 字节的请求得到 `-v 1` 而不是 `-v 0`（零值陷阱的又一例）。
> - **exit 125 必须配 stderr marker** 才算 setup failure —— 否则一个自己退出 125
>   的子进程会被误报成沙箱故障。
> - `rlimit_linux.go` / `rlimit_darwin.go` 的 `setNProc` 已无用途（NPROC 现在走
>   `ulimit -u`），一并删除。
>
> **Windows 侧 fail-closed + 显式逃生阀**（用户裁定）：默认拒绝执行并报
> `ErrLimitsUnsupported`（消息里带上「请求了哪些限制」和逃生阀名字）；
> 本地开发可设 `SANDBOX_ALLOW_UNENFORCED_LIMITS=1` 打开，此时
> `WithOnUnenforcedLimits` 回调会 WARN 一次「本次构建没有资源限制」——
> **可降级，但必须可观测**。
>
> **护栏五处实证**（每处故意破坏 → 确认变红 → 恢复；POSIX 侧在
> `golang:1.25-alpine` 容器里真跑，本机 Windows 无 Linux 能力）：
>
> | 破坏 | 变红 | 保持绿 |
> |------|------|--------|
> | ① 把 setrlimit 塞回父进程（原 bug） | `TestRun_LimitsDoNotTouchTheParent` 的 CPU/AS/NOFILE 三条断言 | 排除该测试后其余 4 条 limit 测试全绿 |
> | ② Windows 恢复成静默 no-op | 4 例（拒绝契约 ×3 + 未生效回调 ×1） | 12 例，含全部零值路径 |
> | ③ 删掉 `ulimit` 的 `\|\|` 失败分支 | 2 例（脚本结构守卫 + 行为级 fail-closed） | 19 例 |
> | ④ `exec "$0" "$@"` 改成字符串拼接 | 2 例（参数保真 + `LimitsReachTheChild` 连带） | 19 例 |
> | ⑤ 去掉 marker 校验（任何 125 都算） | 1 例（误报守卫） | 20 例 |
>
> ①②③⑤ 都是「只红该红的」。④ 的连带红本身是旁证：拼接会把
> `sh -c "…"` 也拆词，说明拼接对真实命令同样有害。
>
> **①的连带现象值得记一笔**：串行跑时 `LimitsReachTheChild` 与
> `WrapperPreservesArguments` 也会红 —— 因为父进程的 **hard limit 被降后无法再升回**
> （非特权进程只能降不能升），后续测试的 `ulimit` 直接 EPERM。这不是测试脆弱，
> 而是**原 bug 的爆炸半径本来就是进程级**的。
>
> **本机能力补充**：POSIX 测试在 Windows 上跑不了（build tag 排除），本机无 gcc、
> WSL 被安全策略禁用。改用 **docker + `golang:1.25-alpine` 挂载宿主模块缓存**
> 在真实 Linux 上执行 —— 这是本次唯一能真正验证 ① 的途径，也为 AUD-12 的
> `-race` 验证提供了可复用的手法（见工作记忆）。
>
> **顺带实测**：
> - 生产值 `RLIMIT_AS = 1 GiB` 对 `go build ./pkg/risk` **够用**（实测通过），
>   所以修好之后不会立刻把构建打挂。
> - `ulimit -u` 的可移植性：**bash ✅ / busybox ash ✅ / dash ❌**
>   （Debian/Ubuntu 的 `/bin/sh` 报 "Illegal option -u"）。生产没设 `NumProcs`，
>   故是地雷而非现患 → 新登记 **AUD-25**。
> - 源码级守卫 `TestNoSetrlimitOnTheParent`：因为行为级护栏只在 POSIX 跑，
>   加了一条**全平台可执行**的源码扫描（包内非测试文件不得出现 `Setrlimit`），
>   让若曦在 Windows 本机 `go test` 就能发现回归。已单独破坏验证过会变红。
>
> **新登记**：AUD-24（Windows Job Object）、AUD-25（`ulimit -u` 在 dash 上不可用）、
> AUD-26（runner 测试在 Windows 上依赖 PATH 里有 POSIX userland）。

> **AUD-12 落地说明（2026-09-21）**：登记只要求「Test 加 `-race` + 新增 frontend
> job」，但**开门前先实测了一次存量**，结果发现竞争，所以本次一并修掉 ——
> 否则门禁上线即红，而那正是登记里担心的情形。
>
> **存量实测**（`go test -race ./...`，docker + `golang:1.25`，gcc 14.2）：
> **17~18 处 `WARNING: DATA RACE`、16 个用例失败，全部集中在 `cmd/analysis`**。
> 竞争地址只有两个，都是 gin 的包级全局：
>
> | 被写对象 | 写方 | 读方 |
> |---------|------|------|
> | `ginMode`（`mode.go:72`） | 测试助手 `rbacTestRouter` / `toolsRBACRouter` 里的 `gin.SetMode(gin.TestMode)` | 另一个并行测试注册路由时 `RouterGroup.handle` → `debugPrintRoute` → `IsDebugging()` |
> | `modeName`（`mode.go:77`） | 同上 | 同上 |
>
> **生产代码无涉**：竞争栈里出现的 `handlers_execution.go:118` /
> `handlers_tools.go:89` 只是**读受害者**（注册路由时读全局 mode）。元凶是测试在
> `t.Parallel()` 下各自调 `gin.SetMode` —— 它用**普通赋值**写全局（不是原子操作），
> 两个并行测试互相竞争，也与别的并行测试的读竞争。
>
> **修法（沿用仓内既有先例）**：`pkg/auth/middleware_test.go` 早在 S7-P0-14 /
> ODR-043 就踩过同一个坑，并在那里写下「Individual tests must NOT call
> `gin.SetMode()` themselves」+ `TestMain` 集中设置。本次把该先例推广到
> `cmd/analysis`：新增 `cmd/analysis/main_test.go` 的 `TestMain`，删掉 15 个测试
> 文件里的 **31 处**逐测试 `gin.SetMode` 调用（含 `handlers_compliance_test.go`
> 的 `func init()` 变体）。**先例存在却没人推广，本身就是这次竞争能活下来的原因。**
>
> **护栏两向实证**：
> - 把 `gin.SetMode(gin.TestMode)` 塞回 `toolsRBACRouter` → `-race` **变红**
>   （EXIT=1、6 处竞争、10 个用例失败，栈指向 `handlers_rbac_test.go` 与 gin 全局）。
> - **同一个破坏，不加 `-race` 时 `ok` / EXIT=0** —— 这正是 AUD-12 的意义：
>   旧门禁对这类竞争**完全不可见**。
> - 恢复后 `go build ./... && go vet ./... && go test ./... -count=1 -race` 全绿。
> - **CI 等价复验（含 Postgres）**：本机裸跑时 `pkg/storage` 的 DB 测试是 `t.Skip` 的
>   （DSN 硬编码 `postgres://postgres:postgres@localhost:5432/quant_trading`），而 CI
>   有 postgres service 容器 —— **两边的实际测试集并不相同**，只按本机结果宣布
>   「CI 不会红」是站不住的。为关掉这个缺口，用「PG 容器与 Go 容器**共享网络命名空间**」
>   （`docker run --network container:<go容器>`，这样容器内的 `localhost:5432` 才落到
>   PG 上）在本地复现了 CI 环境：`-race ./...` 仍全绿（EXIT=0、0 竞争），且
>   `pkg/storage` **76 PASS / 5 SKIP（缺种子数据）/ 0 FAIL** —— 证明 DB 测试真的执行了。
>   （`pkg/storage/integration_test.go` 有 `//go:build integration` 标签，默认
>   `go test ./...` 不编译它，CI 也没带 `-tags=integration`，故不在本次范围内。）
>
> **CI 改动**：`Test` 步骤 → `go test ./... -count=1 -race`（与 AGENTS.md §8 规范 1
> 对齐 —— 该规范早已存在，缺的从来不是要求而是执法者）；新增 `frontend` job
> （node 22 + `npm ci` + lint / typecheck / test，`working-directory: web`）。
> 前端三项**开门前已实测全绿**：lint `0 errors`（820 warnings，退出码 0）、
> typecheck 通过、vitest 15 files / 172 tests 全过；`npm ci --dry-run` 也验过
> lockfile 同步，不会因 `npm ci` 本身失败。
>
> **没有加 `gofmt -l .` 检查**（报告里列为「可选」）：本仓 blob 里存的是 **CRLF**
> 且无 `.gitattributes`，`gofmt -l .` 会在 CI 上列出**全部** Go 文件 → 开门即红。
> 这是既有债，另立 AUD-27。
>
> **新登记**：AUD-27（`gofmt -l .` 在本仓恒失败）、AUD-28（其余 5 个包仍有
> 「测试各自调 `gin.SetMode`」的模式）、AUD-29（`buildRouter` 运行期按日志格式写
> gin 全局 mode）。

> **AUD-13 落地说明（2026-09-21）**：`docker-compose.yml` 的 postgres / redis
> 端口映射由 `"5432:5432"` / `"6379:6379"`（绑 `0.0.0.0`）改为
> `"127.0.0.1:5432:5432"` / `"127.0.0.1:6379:6379"`。应用服务**有意保持
> `0.0.0.0`** —— 它们本来就要被访问。
>
> **redis 的风险比 postgres 大**：本仓 Redis **没有 `requirepass`**，绑 `0.0.0.0`
> 等于把无鉴权缓存交给整个局域网。绑回环是当前唯一有效的访问控制；给 Redis 加
> 密码要动全部服务的 `REDIS_URL`，是另一件事（登记时已注明「另立任务」）。
>
> **⚠️ 本次真正的坑在护栏上，不在配置上**：`tools/check_deploy_consistency.py`
> 的端口正则原来是 `^\s*-\s*"?(\d+):(\d+)"?\s*$` —— 要求整行是**两段**数字。
> 改成三段式 `"127.0.0.1:5432:5432"` 后它**不匹配**，于是 postgres / redis 会
> **静默**从 `compose_svc` 里消失；而这两个服务都在 `ALLOWED_MISSING_IN_K8S` 里，
> 所以护栏**只出一条 note、不报错** —— 看起来还是绿的，其实已经不再检查这两个
> 服务了。**这是「两个机制各自对、接起来就错」的新实例**：配置改对了，护栏瞎了。
> 已改成正则 `^\s*-\s*"?((?:[\w.\-]+):)?(\d+):(\d+)"?\s*$`（宿主绑定地址可选），
> 并新增第 3 条检查：`LOOPBACK_ONLY = {postgres, redis}` 的每条映射都必须绑回环
> （`bind is None` = 绑了 `0.0.0.0` → 报错；绑了非回环地址 → 报错）。
>
> **护栏两向实证**：
> ① 破坏「绑回环」（去掉 `127.0.0.1:`）→ 脚本红且指名端口；
> ② 破坏「回环检查本身」（把 `LOOPBACK_ONLY` 清空）→ 脚本**变绿**，证明该检查
> 真的在起作用（而不是恰好没数据可查）；
> ③ 用旧正则跑新配置 → `parse_compose_ports` 返回的 `compose_svc` 里**没有
> postgres/redis**，脚本仍 `EXIT=0` —— 静默失效的直接证据。
>
> **运行时行为验证（不是看配置文件，是实测）**，用宿主局域网 IP
> `192.168.1.227`（DHCP 的物理网卡地址），探测源是**独立容器**（默认 bridge，
> 非宿主网络命名空间）：
>
> | 目标 | 改前（`0.0.0.0`） | 改后（`127.0.0.1`） |
> |---|---|---|
> | `192.168.1.227:8081`（data-service，对照组） | REACHABLE | REACHABLE |
> | `192.168.1.227:5432`（postgres） | **REACHABLE** | **UNREACHABLE** |
> | `192.168.1.227:6379`（redis） | **REACHABLE** | **UNREACHABLE** |
>
> ① 与 ② 是同一时刻的对照，说明测法有区分度；「改前 REACHABLE」是**临时把
> postgres 改回 `0.0.0.0` 重建后实测**得到的（反向验证，做完即恢复），不是推断。
> 宿主侧 `netstat` 同步印证：`127.0.0.1:5432` / `127.0.0.1:6379` LISTENING，
> `0.0.0.0:8081` 仍是通配。
>
> **没打断本机开发**：容器之间走 compose 服务名，与本映射无关；宿主 `psql` 与
> 测试 DSN（`…@localhost:5432/…`）照旧通 —— `localhost` 就是 `127.0.0.1`。
> 实测 `go test ./pkg/storage/... -count=1` → **76 PASS / 5 SKIP**（与 AUD-12
> 基线一致，不是全 SKIP）。
>
> **⚠️ 测法上的坑**：不要用 `host.docker.internal` 证明「外部不可达」——它是
> Docker Desktop / Rancher 的**宿主侧代理**，转发到 loopback，会**绕过网卡绑定**，
> 测出来必然 REACHABLE，是假阳性。必须用宿主**局域网 IP**，并配对照组。

| ID | 任务 | 位置 | 验收 |
|----|------|------|------|
> **沙箱跨平台一组（AUD-24 / AUD-25 / AUD-26）已于 2026-09-22 全部关闭**，
> 落地说明见本文件末尾「AUD-24 / AUD-25 / AUD-26 落地说明」一节。
>
> **顺带发现并当场落地 AUD-38**：CI 是 Linux-only，全仓 2 个 `//go:build windows`
> 文件在 CI 里**从不被编译** —— 而 `go build` 连 `_test.go` 都不编译，AUD-26 恰恰
> 在这两个测试文件里引入过编译错误、直到在 Windows 上跑才暴露。已补
> `GOOS=windows go vet ./...` 步骤（破坏验证：旧门禁绿、新门禁红）。
>
> **配置一致性三项（AUD-35 / AUD-36 / AUD-37）已于 2026-09-22 全部关闭**，
> 落地说明见本文件末尾「AUD-35 / AUD-36 / AUD-37 落地说明」一节。三项同族：
> **配置项看着有效，却没有读取点**。
>


**ODR-065 的 AUD 线已全部关闭**（AUD-01 ~ AUD-47，2026-09-22）。本批收尾：
**AUD-46**（删除 `pkg/metrics` —— ADR-017 §1 的**竞争实现**，那四个核心指标由
`pkg/observability` 实现，两者同时接线会重复注册 `backtest_duration_seconds` 并 panic；
`internal/repoguard` 新增 **`retiredPackages` 退役负向断言**；`pkg/decimal` 裁决
**保留**并标注「待采用」）/ **AUD-47**（`docs/TEST.md` §5–§7 内容复核）。
落地说明见本文件末尾。

**当时（2026-09-22）剩下的未完成项全在 P2**：P2-1 / P2-2 ⬜ 待做；P2-8 / P2-13 ⛔ 卡在数据同步
（`TUSHARE_TOKEN` 未设置）。⚠️ **本段已过时**：`TUSHARE_TOKEN` 于 2026-09-23 到位、P2-13 已落地、
P2-8 已复核；AUD 线随后又开出 AUD-48~56（其中 48/49/50/53/54/55/56 已关、
AUD-51 代码已落地区，**待办是 AUD-52 与 AUD-51 的真库实测**）。
**以本文件顶部的「状态总览」为准。**

## P2 — 数据与清理

| # | 任务 | 位置 |
|---|---|---|
| **P2-1** | ⬜ 补**宏观/跨境数据源**（美股、汇率、利率、大宗）—— 对产业链认知价值最高 | 新建 adapter |
| **P2-2** | ⬜ 产业链数据底座最小版：`query_supply_chain(name)` | 新建 |
| **P2-3** | ~~因子加 `hypothesis_source` 字段（因果来源）~~ | **✅ 2026-09-18** `pkg/domain/factor.go` + `pkg/storage/factor_hypothesis.go` + `pkg/tools/builtin/factor_hypothesis_tool.go` + `postgres.go`（migration 032） | 新建 `factor_hypothesis` 表而不是给 factor_cache 加列：假设是**因子级**元数据，而 factor_cache 是 symbol×date×factor 的行级缓存，存成列会让同一个字符串重复几十万次。11 个内置因子的假设全部写实（6 个经典来自文献并给出处，5 个桥 B1 纵向因子来自 ADR-022 的产业链推导）—— 不是编数据，它们本来就有明确来源。source_kind 区分 literature / supply_chain / ai_hypothesis / ad_hoc。**有消费者**：新增 MCP 工具 `factor.hypothesis`（Group 11），让 AI 实验员在采用因子前能问「它凭什么有效」；`recorded` 字段区分「库里记过」和「回退到内置默认值」，两者可信度不同。无 DB 时工具仍可用（回退内置表） |
| **P2-4** | ~~幸存者偏差：无退市/剔除逻辑，回测 universe 不完整~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/data/tushare.go` + `pkg/storage/stocks.go` + `pkg/backtest/engine.go` + `engine_daily.go` + `cmd/data/sync_handlers.go` + `cmd/analysis/handlers_explore.go` | 三层一起修：① **数据侧**——`stock_basic` 的 `delist_date` 此前被 `normalizeStocks` 整个丢弃、`Status` 还硬编码 `active`，现在落进 `stocks.delist_date`（migration 030，`*time.Time` —— 零值时间会被读成"一万年前就退市"，必须区分"没有"和"零值"）；同步入口支持 `list_status=ALL` / 逗号分隔，展开成 L+D+P 三次拉取。② **引擎侧**——预热一次上市日历，`eligibleUniverse` 每天把池子过滤成「当日仍在市」：未上市的剔除（未来股，另一种前视偏差）、已摘牌的剔除（那时它已不存在）、**但退市前一直在池子里**（这才是修偏差的关键，只做"剔除"等于把偏差坐实）。③ **持仓**——已退市但还持仓的票保留在 universe 里并在摘牌后强平，否则这笔钱一路挂到回测结束，中间所有损益被抹平。摘牌当天仍算在市（退市整理期有行情）。**没有日历时不过滤**，且偏差维照实报 `PoolSourceCurrent` —— 债还在就别装作修好了。接线后 `ExploreHandler.biasInput()` 按引擎实际口径切换 `PoolSourcePointInTime`，那条每轮都带的 blocking 到此才能真正消失 |
| **P2-5** | ~~废弃模块清理：`pkg/ai/agents` 标记 DEPRECATED 仍是 `cmd/ai` 主链路~~ | **✅ 2026-09-18** 删 `cmd/ai/` + `config/ai-service.yaml` + `deploy/k8s/ai-deployment.yaml` + k8s configmap/ingress 的 ai 条目 + Makefile 的 build-ai/push-ai + 更新 AGENTS.md / ARCHITECTURE.md / docker-compose.yml；改 `pkg/ai/agents/doc.go` | **删的是服务不是能力**：`pkg/ai/agents` 不能删 —— `pkg/ai/pipeline`（P1-1b 探索主链路）在用 ResearchAgent / GenerateAgent / ValidateAgent。真正的问题是 doc.go 那句 "should NOT be used in new code" 是错的并已造成误导，现在改为**分层说明**：ODR-046 废的是**交互层**（前端 AI UI 从未建成），不是**能力层**（本包仍是 pipeline 的实现细节）。`cmd/ai` 删掉的理由：只有 2 条 HTTP 路由且零调用方（前端/后端/编排都没有），没进 compose，只在 k8s 里有部署配置 |
| **P2-6** | ~~`drift` 概念漂移检测零调用（孤儿代码）；`get_market_regime` 未进主流程~~ | **✅ 2026-09-18** `pkg/tools/builtin/strategy_health_tool.go` + `pkg/ai/drift/detector.go` | `get_market_regime` 其实**早已接线**（`cmd/analysis/setup.go` 注册 GetMarketRegimeTool），登记有误。真正剩下的是 `pkg/ai/drift` 与 `pkg/strategy/monitor` 两个零调用孤儿（约 700 行完整实现，互相配套）。接法：写 `driftAdapter` 把 drift 适配成 monitor 的本地 DriftDetector 接口（monitor 故意不 import drift 以免反向依赖），新增 MCP 工具 `monitor.strategy_health`（Group 12）。**顺手修掉一个真 bug**：三个检测方法的 threshold 语义各不一样（mean 比 p 值 / variance 比 logF / distribution 比 KS 统计量），**没有任何取值能同时让三者合理** —— 传 2.0 会让 mean 把一切都判成漂移而 distribution 永不触发（KS≤1）。现统一为 `pValue < threshold`。与稳健维不重复：稳健维看参数邻域和分年度一致性（回测内部性质），这里看时序上最近是否偏离历史（上线后才有的问题）。样本不足时如实说"判断不了"，不假装健康 |
| **P2-7** | ~~死配置 `config/ai-service.yaml` 从未被读取~~；~~`docker-compose.services.yml` 引用不存在的 Dockerfile~~（后者已于 2026-09-18 删除，见 P1-10） | **✅ 2026-09-18** 随 P2-5 删除 | ai-service.yaml 是 `cmd/ai` 的配置，而 cmd/ai 零调用方且已删；另三个（analysis / data / strategy-service.yaml）都确实被读，不动 |
| **P2-8** | ✅ **2026-09-23 复核完成** —— 幸存的前视风险复核：复权口径无 hfq 对照。**取证 = `pkg/data/tushare_adjustment_integration_test.go`（真库 + 真源 + 真引擎）；结论见本文件末尾「P2-8 取证说明」。hfq 落库转由 AUD-53 阻挡** | **原登记写 `migrations` —— 这条是错的**（本仓 `migrations/` **不执行**，DDL 唯一真相在 `pkg/storage/postgres.go` 的内联数组）。真正的落点：`pkg/data/tushare.go`（复权计算）+ `pkg/storage/postgres.go`（表结构） | **原方案已被实测推翻**：登记设想的「同一个 `stk_factor_pro` 多要 4 个 `*_hfq` 字段」不可行 —— 该接口返回 **40203 无权限**。**新事实**：`daily` + `adj_factor` 两个接口都有权限，组合起来**能自己同时算出 qfq 和 hfq**（公式与官方算例见 `docs/guides/data-dependencies.md`）。当前状态：**qfq 已落 `ohlcv_daily_qfq`（回测口径不变），hfq 未落库**。⚠️ **下面的判据是分析、不是实测**（落地时要实测证伪）：qfq 与 hfq 只差一个**每股常数**（最新复权因子），故**收益率序列完全相同** → 前复权的前视成分只落在**价格水平**上，对「按收益率排序」的信号无影响；可能有实质影响的是**按股数下单**（`floor(现金/价格)`，用的是被未来事件改写过的历史价格水平）。这条成立与否决定了要不要真落一套 hfq 数据。**⚠️ 2026-09-23 实测：这条判据方向对、量级错。** 换口径确实实质影响结果（收益差 **21.6 个百分点**），但**通道不是取整** —— 资金放大 100 倍后差异不减反增（24.9pp），而把「每票各自的常数」换成「全体同一个常数」（同样量级 ×9.04）差异只剩 **1.1pp**。**⚠️ 2026-09-24 更正**：上一版此处断言「通道在**跨票价格水平**上」，**已被证伪** —— regime 检测对每票各自缩放实测 14/14 窗口判定完全一致（`pkg/risk/regime_scale_invariance_test.go`）。真正的通道在下单侧：策略无状态 + 引擎的「目标 − 已持」抵扣只在 `PendingQty > 0` 时生效 → 每天重发全量买单、每天被拒。已登记 **AUD-53**（不稳健）与 **AUD-55**（重复下单缺陷，AUD-53 的根因）。**⚠️ 2026-09-24 二次更正**：AUD-55 **已修**（抵扣改成无条件 + 已持仓改为实时读 tracker），资金侧的敏感度随之塌掉（合成 4.82pp → 0.76pp、阶梯转收敛、零拒单）；剩下的价格侧差异被确认为**绝对金额约束**（一手 = 100 × 价格，而资金固定），**不是引擎缺陷**。**由此 hfq 落库的判据换过了**：hfq 价 = qfq 价 × 每股常数、**单位已经不是「元」**，固定资金下会**改写票池成分** —— 落之前必须先回答「资金要不要按同一基准缩放」，详见 `docs/guides/data-dependencies.md` 与文末「P2-8 取证说明」五。 |
| **P2-9wire** | └ **接线**（聚合 → 循环 → 日志 → 前端）：**✅ 2026-09-18** `aggregate.go` + `pkg/ai/loop` + `handlers_explore.go` + `web/src/pages/Explore.vue` | 五片全写完时整包零调用（"5/6 片的零件 + 0 接线"）。聚合入口 `ValidateProposal` 取**各维最小值**作综合概率（合取：算术平均会让四维优秀掩盖一维致命，几何平均稀释太狠）；循环控制器**每次尝试后**跑（不是跑完整轮才跑），邻域 = 本轮已试过的参数；裁决写回 `experiments.verdict`（migration 029，`json.RawMessage` 以避免 storage→validation 成环）；偏差维按实测口径注入（前复权 + **池子按当前上市名单** = 幸存者偏差真实存在，所以每轮都会带这条 blocking —— P2-4 那笔债就该长这样）。踩过的坑：`Attempt` 是值类型，`append` 早于 `Verdict` 赋值会让库里那一版永远为空 |
| **P2-9a** | └ **统计**：多重检验校正（试了 N 次，门槛按 N 收紧）。吃 P1-1 的实验日志 | **✅ 2026-09-17** `pkg/validation/statistical.go` | 同样 Sharpe，试 500 次必须比试 5 次更不可信 |
| **P2-9b** | └ **经济**：扣费后净收益 | **✅ 2026-09-17** `pkg/validation/economic.go` + `turnover.go` | 毛收益扣掉手续费 / 印花税 / 过户费 / 冲击成本后仍成立；附盈亏平衡换手率 |
| **P2-9c** | └ **稳健**：参数敏感度（高原面积）+ 分年度一致性 | **✅ 2026-09-17** `pkg/validation/robustness.go` | 邻域不能塌（高原优于尖峰）、收益不能集中在单段；中心太低的高原按高度打折 |
| **P2-9d** | └ **偏差**：前视、幸存者、复权口径 | **✅ 2026-09-17** `pkg/validation/bias.go` | 三子维度几何平均（合取：一维致命就拉垮）。**没查 ≠ 没问题**：未评估的子维度不参与概率，只给 note；一个都没评评估时概率是中性 0.5。接线状态已写进文件头：行情是前复权（`ohlcv_daily_qfq`，自带前视成分）、池子来源取决于同步时的 `list_status`（传 "L" = 当前上市 = 真有幸存者偏差）、PIT 目前只有 equitydeep 链路强制 `ann_date` |
| **P2-9e** | └ **冗余**：与已有策略相关性（防「伪分散」） | **✅ 2026-09-18** `pkg/validation/redundancy.go` | 分散的是风险来源不是策略数量。概率按样本量向 0.5 收缩（n<30 不敢断言）；**零方差序列记入 Skipped 而非当成 ρ=0**（那会伪装成「完全不相关」）；强负相关标明是「反向复制」不是新增风险源。接线口 `RedundancyFromBacktests` 直接吃回测结果。取证：lookback 20 vs 25 → \|ρ\|=0.77 判伪分散；vs 200 → 0.30 |
| **P2-9f** | └ **因果**（L3，需 LLM）：讲得出为什么吗 | **✅ 2026-09-18** `pkg/validation/causal.go` + `pkg/ai/causal/narrator.go` | **形态不是"让 LLM 讲讲为什么"，而是"先下注、再验证"**：模型给机制 + **可证伪的预测**（带数值边界），确定性检验逐条验，讲得出但预测不中的等于没讲。硬规矩：给模型的请求里**物理上不含回测结果**（`ValidateCausal` 里 `blind := req; blind.Result = nil`）—— 知道答案后做的"预测"只是复述，那一维会永远通过。可验种类：胜率 / 成交笔数 / 持有天数 / 最大回撤 / 收益集中度（top-K 天贡献占比）。概率 = Beta(1,1) 后验均值 `(中+1)/(可验+2)`；**一条可证伪的都没有 → 0.25 并 blocking**（"只讲了散文"）；验不了的预测既不算通过也不算失败，只给 note。接线节奏：一次模型调用不便宜，所以**一轮探索只给最终候选做一次**（`AttachCausal` 事后补进裁决并重算综合概率 / 最弱维 / blocking），叙述失败 = 未评估，不假装通过 |
| **P2-10** | ~~`domain.Fundamental` 数值字段是 `float64`，而表中列可为空。P0-1 中用 `COALESCE(col,0)` 兜底，导致**缺失值被当作 0 而非"未知"**（PE=0 会被误判为极便宜）~~ | **✅ 2026-09-18** `pkg/domain/market/types.go` + `pkg/storage/fundamentals.go` + `pkg/data/tushare.go` + `pkg/strategy/examples/value_momentum.go` | 两层都改了：① 类型改 `*float64`，存储层**去掉** `COALESCE(col,0)`（NULL 扫成 nil），源端缺字段也留 nil（`fieldFloatPtr`）；② 因子层显式跳过缺失：`stockFactorData.PE/PB/ROE` 也改指针，`calculateMeanStd` 只统计非 nil（此前靠 `v != 0` 近似"缺数据"，而 ROE=0 是盈亏平衡，是真值），缺项的股票在对应因子上得**中性 0 分**而不是"最便宜"。这个 bug 的杀伤力在于它会主动把人引向错误交易：PE 缺失折成 0 后在"越低越便宜"的排序里冲到第一 |
| **P2-11** | ~~Hermes Agent 系统设计文档遗失（原在 `.trae/documents/`，目录已删）。SPEC §6 与 hermes 验收测试均引用它~~ | **✅ 2026-09-18** `docs/hermes/system-design.md` | 遗失的是 `hermes-agent-integration-system-design.md`，代码里有 4 处引用（gate.go §6.2、factor_tools/gene_pool_tools/walkforward_tool §3.2）。**从代码反推补齐**：源码注释里逐条记了「设计说 X，我们做了 Y，原因 Z」，提炼出来就是 §3.2（工具参数契约 + 3 处已记录的偏离）与 §6.2（GateDecision 门控元数据）。**只有这两节是复原的**，其余章节代码没引用就不凭空补写，并在文档头部写明这是反推而非原件 |
| **P2-12** | ~~**表达式引擎只暴露 OHLCV**（open/high/low/close/volume/turnover），因此 `value` / `quality` 类意图表达不出 —— P0-5 中它们只能明确失败，而不是套一个无关的价格表达式产出误导性回测数字~~ | **✅ 2026-09-18** `pkg/strategy/expression/data_provider.go` + `strategy.go` + `pkg/strategy/strategy.go` + `pkg/storage/fundamentals.go` + `pkg/backtest/engine.go` + `pkg/ai/yaml/generator.go` | 新增 `pe/pb/ps/roe/roa` 五个字段，**按 PIT 对齐**（`GetFundamentalsPITBulk` 返回的 Date 是可用日 `COALESCE(ann_date, trade_date)`，不是报告期；`OHLCVDataProvider.fundamentalSeries` 按每根 K 线的日期切一刀，取不到填 NaN 不是 0）。注入走 `strategy.FundamentalAware` 可选接口（`GenerateSignals` 签名没有基本面参数，不动接口；范式同 `FactorAware`），且**只有声明要财报的策略才预热**——纯价量策略不付这份查询成本。`value` → `cs_rank(neg(pe)) + cs_rank(neg(pb)) > 1.6`，`quality` → `cs_rank(roe) + cs_rank(roa) > 1.6`；`custom` 仍明确失败。**估值倍数非正一律 NaN**：PE 为负不是"便宜"是亏损，`neg(pe)` 不该把亏得最狠的排成最便宜（经典价值陷阱）；ROE/ROA 为负是真实的差，原样保留。**顺手修掉一个潜伏 bug**：`neg(x)` 的函数形式此前从未接上（`evaluateFunction` 无条件走 `applyTimeSeriesOp`），`multi_factor` 的默认表达式 `cs_rank(neg(ts_std(close,20)))` 从落地起就是「解析得过、跑不起来」—— 它只被断言过能解析，从没被求值过 |
| **P2-13** | ~~**验证器链缺真实回测的端到端取证**~~（2026-09-17 已解决一半：引擎三处 HTTP 都有 in-process 分支，`SetRiskManager` 一接就能完全离线；当时「离线跑不了」是误判） | **✅ 2026-09-23** `pkg/validation/live_backtest_integration_test.go`（新建，1 例） | **登记的「等全量同步」是过度前置**：验证器链接真库**不需要 5907 只票** —— 有 65 只票带 3 年完整历史时就够取证了。新测试接**真库**（`storage.NewPostgresStore` + `marketdata.NewPostgresProvider`；**注意不是 `testutil` 的 `quant_trading_test`** —— 连错库会得到一个空库，然后以「库里没数据」skip，永远绿），跑真引擎回测再过五维验证器。**为什么不能只用合成数据**：合成行情每只票都整齐地有 252 根 K 线，证明不了真实数据的形状（稀疏 / 停牌 / 复权 / 退市 / 上市日期参差）不会把链路打崩。**首次实测：验证器链在真数据上否掉了这个策略** —— 综合概率 0.0000、3 条 blocking（统计维校正后 p=0.857 / 经济维净 -39.04% / 稳健维邻域 0% 站得住），偏差维 0.8746、冗余维 0.7044、因果维如实记未评估。取证细节见本文件末尾落地说明 |

**2026-09-21 全栈审查（ODR-065）新增 5 项**（Medium/Low）—— **AUD-14 ~ AUD-18 全部完成**：

> **AUD-14 落地说明（2026-09-21）**：AGENTS.md 从 v3.3 升到 **v3.4**，顶层叙述整体
> 从 ADR-022（双对等工作面 + 飞轮闭环，从未实施）切换到 **ADR-023/024 的现实**
> （一间单人自托管实验室：三角色 + 三层模型 L1/L2/L3 + 执行载体是表达式）。
>
> **⚠️ 登记里的「内联 DDL 22 张」是审查时点的数字，照抄会写错。**
> 实测演变：`06e9945`（审查时）= 22 → C5 补 11 张多源目标表（`a1ef447`）= 33 →
> 再补 4 张代码直接引用的表（`02640bb`）= **37**。已按现值写成
> 「**内联 DDL 37 张（唯一执行路径）**」，并把 `32 → 38 → 39 → 22 → 37`
> 这条快照链写进文档，注明**数表就 grep 源码，别引用文档里的数**。
> 同时删掉「内联 N + 迁移 M = 总数」这个口径 —— `migrations/` 与
> `docs/migrations/` **不被执行**（`migration_manager.go` 已于 2026-09-18 删除），
> 那一半从来不参与建表，加进去只是把两个不相干的数拼成看起来合理的数。
>
> **`--include-archive` 开关早就有了，但零调用方** —— 又一个「零件已存在、没接上」：
> CI 跑的是默认模式（跳过 archive/），而 ODR 与报告全在 archive/，死链永不被告警。
> 已接进 CI（新增一个 step），并修好它立刻暴露的 **7 条坏链**：
> `odr-044`→ADR-020 文件名已改、`odr-065`→ODR-043 文件名顺序错、
> `FINAL_VERIFICATION_REPORT`→`CLAUDE.md`（工具适配层已删，改为去链接 + 加注）、
> `review-report` 与 `meta-review` 归档时相对层级没同步（各 2 条）。
>
> **顺带修了检查器本身的一个误报**：`[x](y.md)` 出现在**代码块 / 行内代码**里时是
> **引文**（meta-review 在表格里引用坏链当证据），不是导航链接。原来照样告警，
> 修好它反而等于抹掉证据。已让检查器跳过代码段，7 条 → 5 条真死链。
> 护栏双向验证：破坏 archive 里一条真链接 → 退出码 1 且指名文件；往行内代码里塞一条
> 死链接 → 仍为 0。
>
> **波及面比登记的大**（`docs/ARCHITECTURE.md` 有同一处 M2 缺陷，而 AGENTS.md §11
> 正是引用它给表数，不改会造出新的不一致）：表数段落整段重写为 37 张；
> 顺带修了 3 处会被读者照着行动的事实错误 —— `ai-research-service :8086` 标
> 「✅ 运行中」（2026-09-18 已删除）、`equitydeep-research` 标「Proposed」（ADR-023
> 已取消）、`fundamentals_detail` 标「未落地」（表已建，是**数据**没摄取）；
> 并标注 `data_source_registry` / `data_fallback_chain` **从来不是表**（内存态）。
>
> **有意不改**：`docs/AGENTS_TEMPLATE.md` 里的 `docs/odr/` 与 `docs/decisions/`
> 是**通用模板的占位约定**（文件头写着「适用场景：任何项目」，正文用 `[ProjectName]`），
> 不是本仓声明 —— 改它反而把模板特化成本仓结构。
>
> **新登记**：**AUD-30**（`docs/SPEC.md` 仍按 ADR-022 定版，未反映 ADR-023/024）。

> **AUD-15 落地说明（2026-09-21）**：两项。
>
> **① 删 `e2e/tests/ai-research.spec.ts`**。它是全仓**唯一**打 `:8086` 的 spec
> （3 处），前端早已没有 `/ai-research` 路由，`cmd/ai` 也于 2026-09-18 删除。
> **CI 不跑 e2e**，所以它不红在 CI，只污染本机「全绿」这个信号 —— 每次本地跑
> playwright 都看到失败，久了就没人再信这个套件。其余 spec 打的是 `:8085`
> （analysis service，存活），`waitForBackendReady` helper 也走 8085，本次只删
> 这一个。
>
> **② `fundamentals_detail` 空集防御**。原行为：读到 0 行 → 空 book → 每个公式
> 跳过每只股票 → `saveVerticalFactor` 记一条 Warn 后返回 nil →
> `POST /sync/factors/:name` 回答 **200 "factor computed and cached"**，而实际
> 一行都没算。**这个响应不是对真值的近似，是它的反面。**
>
> 新增哨兵 `ErrNoFundamentalsDetail`，`loadStatementBook` 读到 **0 行**即返回它
> （包在 `%w` 里，可 `errors.Is`）。边界是刻意画的：
>
> | 情形 | 判定 | 理由 |
> |---|---|---|
> | 0 行 | **错误** | 源根本没在喂数据（EQD-P1-2 未落地就是这状态） |
> | 有行、但全被过滤（nil value / 非季末） | **不是错误** | 源是好的，只是这一天没有股票历史够长；现有 `drops rows without a value`、`drops periods that are not quarter ends` 两个用例钉住了它 |
>
> 「0 行就硬报错」不能一路往上传：`ComputeAllFactors` 若照单全收，
> `POST /sync/factors/all` 与 `ComputeFactorsForRange` 的日期循环会**整体失败**，
> 而 `fundamentals_detail` 现在就是空的 —— 等于横截面因子天天跟着陪葬。
> 故 `ComputeAllFactors` 认得这个哨兵：**跳过并记进报告，不失败**。
> 签名随之 `error` → **`(FactorBatchReport, error)`**，报告里是 `Computed` 与
> `Skipped`（含每个被跳因子的原因）。少了报告，「没报错」会被读成
> 「八个因子都缓存好了」—— 正是 `loadStatementBook` 那个空 book 说过的谎，
> 往上一层。端点据此返回：
>
> ```
> {"message":"3 of 8 factors computed, 5 skipped","computed":[...],"skipped":[{"factor":...,"reason":...}]}
> ```
>
> 顺带把并行路径的报错次序定下来：原来「channel 里第一个到的错误」胜出，
> 哪个 goroutine 先跑完不是数据的属性，失败原因因此不可复现。现在结果按
> **列表序重排**再挑选，并行与串行报同一个因子。
>
> **护栏三向破坏验证**（都做到「只红该红的」）：
>
> | 破坏 | 红了什么 | 仍然绿 |
> |---|---|---|
> | 删掉 0 行哨兵检查 | 5 个新用例（含 2 个 handler 用例） | 全部存量用例 |
> | 批量路径不认哨兵（`errors.Is` 永不成立） | 仅 2 个批量用例 | 单因子哨兵、真实错误传播 |
> | 去掉并行结果排序 | 并行确定性断言（`-count=30` 必红） | 其余 |
>
> **新增测试**：`pkg/data` 3 个（五个纵向因子逐个验哨兵 + 不写库、
> 「0 行 vs 全被过滤」的边界对、批量跳过报告串行/并行一致）；
> `cmd/data/handlers_factor_test.go` 2 个 —— 单因子端点 500，
> 并**配对照组**（`momentum` 同一空 store 仍 200），否则 500 什么也证明不了
> （可能只是所有请求都挂）。
>
> **有意不改**：`saveVerticalFactor` 的「有数据但算不出 0 值」仍是 Warn + nil ——
> 那是数据可得性，不是源断流，改它会把「这一天没有合格股票」变成故障。
> handler 路由注册在 `main.go`（不在 `buildRouter`），测试用手写 router，
> 钉的是 handler 不是 wiring。

> **AUD-16 落地说明（2026-09-22）**：登记只要求「补复核」—— 逐行读组合状态更新
> 路径，确认是否真有分支导致状态不更新，坐实则升级为缺陷并定级。
> **坐实，而且比登记说的更狠：不是「某分支不触发」，是整条路径都不落地。**
>
> `updatePortfolio()` 从 `positionManager.GetPositions()` 拿到的是**拷贝切片**
> （`append(result, position)` 值拷贝），三个赋值全写在拷贝上、从不写回 →
> `CurrentPrice` / `MarketValue` / `UnrealizedPnL` **恒为 0**，
> `GetTotalMarketValue()` / `GetTotalUnrealizedPnL()` 也跟着恒为 0。
> 实证方式是**先写测试再修**（红 → 绿）：12.5 → 0、1250 → 0、250 → 0。
>
> 修法：新增 `PositionManager.ApplyQuote(symbol, price)`，在**同一把写锁里**
> 读-改-写 —— 不做成「GetPositions → 改 → UpdatePosition」，那样会把夹在
> 中间的成交（或 broker 持仓同步）覆盖掉，是同一个 bug 往上一层。
>
> 顺带挡掉两种「不是价格的价格」：
>
> - `GetQuote` 报错 → 保留上一次 mark，下一轮 ticker 重试（报错是「没有价格」）；
> - `Close <= 0` → 同上。**本仓自己的 `errDataFeed.GetQuote` 就返回零值 Quote
>   且不带 error**，照写会把 mark 抹成 0（「价格为 0」在 A 股不是合法价格）。
>
> **定级 Medium（不是 Low）**：功能是死的，但当前**爆炸半径为零** —— 唯一消费方
> `handlers_paper_trading.go` 的 `/api/paper/*` 端点**未在 `main.go` / `setup.go`
> 注册**（AUD-19 的死代码）。一旦有人接线，拿到的就是全 0 的估值与全 0 的汇总。
>
> **新登记 AUD-31**：`e.portfolio` 从构造后**从未被更新**（见下表）。本次不扩大
> 改动 —— 它与 AUD-19（删 paper 端点）/ AUD-23（`GetPortfolio()` 无锁）绑在一起，
> 端点删了之后它可能直接变成零调用方死字段。

> **AUD-17 落地说明（2026-09-22）**：登记只要求「注释声明威胁模型」。落地时发现
> **这个包一个测试都没有** —— M4 的绕过说法（报告里也是转述 D4 子代理）从来没被
> 验证过。所以先补测试把能力边界钉成可执行证据，再写注释。
>
> **实证：四种绕过全部成立**（都是合法 Go，不是假想）：
>
> | 绕过 | 例子 | 为什么过 |
> |---|---|---|
> | 别名导入 | `import fs "os"` → `fs.RemoveAll(...)` | 正则认的是字面量 `os.RemoveAll(` |
> | 点导入 | `import . "os"` → `RemoveAll(...)` | 限定符整个没了 |
> | 变量间接调用 | `rm := os.RemoveAll; rm(...)` | 标识符出现在赋值处，不在调用处 |
> | 跨行拆分 | `rm := os.` ⏎ `RemoveAll` | **本实现特有**：为了 `Finding.Line` 精确而逐行扫描，副作用是跨行的选择器永远不成 token |
>
> 第四种是本次新发现的 —— 报告只列了「包别名 / 变量间接 / 反射 / 字符串拼接」，
> 没提这个，而它是**为了让行号精确而付出的代价**，属于实现选择的副作用。
>
> 反方向也一并钉住：注释被剥离（提到 `os.RemoveAll` 的散文不误报），但
> **字符串字面量不剥离** —— 写着「never call os.RemoveAll()」的合法策略会被拒。
> fail-closed 下这个取舍是对的（误报代价是一次构建，漏报代价是一台主机），
> 但应该被人知道，而不是被撞见。
>
> 威胁模型写成 **WHO / NOT WHO** + ✅❌ 清单，**沿用 `internal/sandbox/runner`
> 已有的体例**（先例推广）：防的是「不知道沙箱约定的 LLM」（对手属性是粗心不是
> 恶意），**不防**「知道这里有门禁的人」。并写明**真正的边界是 layer 2 的
> runner**（子进程 + rlimit），本门禁只决定「要不要构建」—— 扫描干净 ≠ 可以跑。
>
> 顺手改了 runner 文档里那句「catches patterns that WOULD lead to those
> escapes」：那会让人以为真能拦住，已改为「只是字面形式的便宜前置过滤」并指回
> 本包文档。
>
> **护栏两向破坏验证**（证明这些测试真有约束力，不是恰好通过）：
>
> | 破坏 | 红了 |
> |---|---|
> | 正则去掉 `os.` 前缀 | 只有 `aliased_import` 红（其余三种绕过机制不同，不受影响 —— 「只红该红的」） |
> | 去掉注释剥离 | `TestCheck_IgnoresComments` 红 |
>
> 测试里留了显式提示：将来若用 go/ast / gosec 补上这些绕过，
> `TestCheck_DocumentedBypasses` 会失败 —— 那是**提醒你在同一次改动里更新威胁
> 模型**，不是要你删测试。

> **AUD-18 落地说明（2026-09-22）**：若曦裁决 **B —— 分阶段退役**。本次只落地
> 「裁决 + 时间表」，**代码一行未动**。
>
> **⚠️ 关键勘察结论：legacy 不是死代码，不能按死代码处理。**
> `main.go:287-320` 注册了 **10 条路由**（`/`、`/screen`、`/dashboard`、`/copilot`、
> `/strategy-selector` 各带 `.html` 变体）+ `/static` 静态目录；打的是**活着的**
> `/api/strategies`（handlers_strategy.go:14）与 `/api/copilot/*`
> （handlers_copilot.go:198），**没有打已删的 :8086**。所以删它是用户可见的
> 行为变更 —— 与 AUD-19 那批「零调用方端点」不是一个性质。
>
> **三处连带**（删 legacy 会一起动，不是删 6 个文件）：
>
> 1. **4 条裸镜像路由**（`/ohlcv/:symbol`、`POST /screen`、`/stocks/count`、
>    `/market/index`）只被 legacy 消费 —— ODR-062 取证 e 已记「与 legacy 共存亡」；
> 2. `cmd/analysis/deps_test.go:172` 的 `GET /static/*filepath` 路由断言；
> 3. **compose 里根本没有 SPA 部署**（只有一行 CORS 注释提到 `:5173`）→
>    legacy 是**当前唯一的服务端 UI**（ODR-062 取证 e）。
>
> **这不是第一次裁决**：ODR-062（2026-09-16）的 S-D 项若曦已选「保留」，理由正是
> 「compose 无 SPA 部署」，并写明「legacy 退役**另立议题**，本切片不夹带」——
> **AUD-18 就是那个议题**，现在接续。
>
> **时间表（已写入 AGENTS.md §14）**：
>
> | 阶段 | 内容 | 任务 |
> |---|---|---|
> | ① 先决 | 补 Vue SPA 部署（`(a)` compose 加 web 服务 / `(b)` Go embed 托管 dist —— 这是个待定的决定） | **AUD-32** |
> | ② 冻结 | AUD-32 一完成，legacy 即冻结：只许不动，不再改 | — |
> | ③ 删除 | 按上面的连带清单一并删除 | **AUD-33**（依赖 AUD-32） |
>
> **顺序不能反**：先删会有一段「`:8085` 打开只剩 API」的空窗。
>
> **一个说明定位的细节**：`index.html:192` / `dashboard.html:318` 硬编码
> `API = 'http://localhost:8085'` —— 这套页面**本来就只有本机能用**，部署到别的
> 机器就坏（copilot.html / screen.html 用相对路径 `API=''`）。它的实际定位更接近
> 「本机运维 / 调试台」，而不是日常 UI。

> **AUD-32 落地说明（2026-09-22）**：若曦裁决采用 **独立 web 服务**（不是 Go 侧托管
> `dist`），前端已在 **宿主 8080** 上线。`web/`（Vue 3 + Vite，13 个页面）此前
> **没有任何部署** —— 只能 `npm run dev` 跑在 `:5173`，这就是 legacy 删不掉的根因。
>
> **新增/改动的文件**：
> `Dockerfile.web`（node 构建 → nginx，dist 在镜像内重新构建）、
> `deploy/nginx-spa.conf`（SPA 回退 + `/api` 反代 + SSE 无缓冲）、
> `docker-compose.yml` 的 `web` 服务、
> `deploy/k8s/web-deployment.yaml`（新）、`deploy/k8s/ingress.yaml`（改）。
>
> **⚠️ 勘察时发现一处既有矛盾**：`deploy/k8s/ingress.yaml` 原写着
> 「Main API + Vue SPA served by analysis-service (port 8085)」—— **k8s 侧的
> 既定假设是「Go 侧托管 SPA」**（即另一个方案）。只改 compose 不改 ingress，
> k8s 路径下打开就是一份没有 SPA 的 analysis，而 compose 路径却是好的 ——
> 典型的「两边各自对、接起来就错」。已把 ingress 的 `/` 改指 `web:8080`，
> 并**删掉 `rewrite-target: /`**：配合 `path: /` 它会把所有 URI 重写成 `/`，
> `/api/health` 到后端就只剩 `/` 了。
>
> **三个关键设计**（详见 `docs/guides/deploy-config.md` 的「前端 web 服务」一节）：
>
> 1. **由 nginx 反代 `/api` → `analysis-service:8085`**，不让前端直连。前端
>    API 基址默认空字符串（`web/src/api/client.ts:65`），请求走相对路径；
>    反代后浏览器侧仍是**同源**，所以**前端零改动、CORS 也不用动**
>    （`SERVER_CORS_ALLOWED_ORIGINS` 保持留空 = 最安全）。
> 2. **反代目标用 `resolver` + 变量，不写死域名**。写死时 nginx 只在启动时解析
>    一次：后端晚起来会让 nginx 启动失败，后端重启换 IP 后仍打旧地址。
>    resolver 写了 `127.0.0.11`（Docker）与 `10.96.0.10`（k8s CoreDNS 默认）。
> 3. **SSE 必须显式关缓冲**：同步进度用 `EventSource`
>    （`web/src/stores/sync.ts:162`），nginx 默认缓冲会把事件攒到最后一次性下发
>    —— 前端表现是「进度条一动不动，跑完才跳到 100%」。
>
> **实证（本机 docker，非推断）**：镜像构建通过（1m03s，含 `vue-tsc` 类型检查）；
> `/` → index.html、深链 `/backtest` → 回退到同一份 index.html、
> `/api/health` → 后端收到**完整** URI（`{"path":"/api/health"}`，证明
> `proxy_pass http://$backend$request_uri` 里的 `$request_uri` 不能省）、
> `nginx -t` 通过。SSE 做了**破坏验证**：`proxy_buffering off` 时 2 秒窗口收到
> 5 条事件；改成 `on` 后收到 **0** 条；恢复后又是 5 条 —— 证明那条配置真的在起作用，
> 而不是「刚好没缓冲」。
>
> **⚠️ 未实证的部分**：**k8s 侧只做了配置对齐，没有集群可跑**。
> `web-deployment.yaml` 与改动后的 `ingress.yaml` 属「按同一套约定推出来的」。
> 首次真跑 k8s 时重点看三处：`quant-trading/web:latest` 镜像是否存在、
> CoreDNS 地址是否为 `10.96.0.10`、ingress 去掉 `rewrite-target` 后
> `/api/*` 是否完整透传。
>
> **顺带记一个护栏的边界**：`check_deploy_consistency.py` 只比对 compose 与
> k8s **Service** 的端口，**不管 ingress 的 backend** —— 改 web 端口时漏改
> `ingress.yaml` 不会报错，只会让 k8s 路径下前端打不开。这条已写进
> `docs/guides/deploy-config.md` 的端口清单（第 5 步）。

> **AUD-19 落地说明（2026-09-22）**：整条 `/api/paper/*` 已删除。
>
> **⚠️ 登记的判断被推翻 —— 但结论没变、理由变了。** 登记（AUD-02 顺带发现）
> 写的是「零调用方死代码、全仓无前端/文档引用」。逐项核实后发现**前半句对、
> 后半句错**：
>
> - `registerPaperTradingRoutes` 确实**零调用方** —— 10 个端点从未挂到任何 router；
> - **但前端有完整的「模拟交易」页面**：`PaperTrading.vue` + `/paper-trading`
>   路由 + 两个导航入口（NavTiles / AppSidebar）+ `usePaperTradingData` 并发调
>   4 个 API；`docs/SPEC.md` 还有一整节在描述这 10 个端点。
>
> 所以真正的结论比登记写的更强：**不是「没人用」，是「有人在用，而后端从未挂上
> → 那个页面打开就是 404」**。功能从未上线过 —— 不是「坏了」。
>
> **裁决依据（若曦裁定「删整条线」）**：
>
> | 依据 | 证据 |
> |---|---|
> | 能力已被活的端点覆盖 7/10 | orders / orders/:id / orders/:id/cancel / positions 与 `/api/execution/*` 一一对应，`/account` 对应 portfolio；且那套**带 RBAC**（`requireTrader`） |
> | 剩下 3 个无生产用途 | start / stop / status 是「模拟会话生命周期」，`MockTrader` 那套不需要这个概念 |
> | 底层是测试脚手架 | `SimulatedDataFeed` 文件头自己写着 `for testing` —— 不是行情接入，即使挂载也没有真实数据源 |
> | 顺带关掉两个缺陷 | `GetPortfolio()` 恒返回「初始资金 + 空持仓」（AUD-31）、无锁返回内部指针（AUD-23） |
>
> **删除清单（前后端两侧）**：
>
> - 后端：`cmd/analysis/handlers_paper_trading.go`（整文件 282 行）；
>   `LiveEngine.portfolio` 字段 + `GetPortfolio()` 方法（AUD-31 / AUD-23 的验收：
>   该字段与端点一并删除）；`live_test.go` 两处随之调整（一处断言测的正是那个
>   恒假读数，改成断言 `positionManager` 的真实汇总）。
> - 前端：`pages/PaperTrading.vue`、`composables/usePaperTradingData.ts`（+ 测试）、
>   `api/paper-trading.ts`、router 的 `paper-trading` 路由、NavTiles 与 AppSidebar
>   的「模拟交易」入口（顺带清掉随之未使用的 `TrendingUpOutline` / `CashOutline`
>   import）。
> - 文档：`docs/SPEC.md` 第 5 节改写为「已删除」并保留订单类型语义；
>   `docs/openapi.yaml` 的 `paper-trading order` 措辞澄清（它指的是 execution 端点，
>   不是被删的那批）。
>
> **⚠️ 一个连带决定**：`EmergencyFlatten.vue`（kill-switch）**只被 PaperTrading.vue
> 引用** —— 删页面会让它成孤儿组件，但它打的 `/api/execution/emergency-flatten`
> 是**活的**。若曦裁定**挂到控制台**：现已移到 `Dashboard.vue` 页尾（危险操作不放
> 显眼处），`@flattened` 事件不监听（控制台没有持仓视图）。`api/paper-trading.ts`
> 里唯一活着的东西（`emergencyFlatten`）搬到新建的 `web/src/api/execution.ts`。
>
> **顺带修正两处过期叙述**：AUD-02 落地说明里「全仓无前端/文档引用」已标注为错误；
> AUD-10 落地说明里「`handlers_paper_trading.go` 的 HTTP handler 同时读」已删除
> （它当时根本没挂载，那次列举里只有 execution 一侧是真的）。
>
> **验证**：`go build` / `go vet` / `go test ./...` 全绿；前端 `typecheck` 通过、
> `lint` **0 errors**（714 个格式 warning 是仓内既有）、`npm test` 14 文件 161 测试
> 全过；全仓 grep 确认 `/api/paper`、`PaperTrading`、`GetPortfolio` 无残留引用
> （剩下的是注释与 `pkg/backtest/tracker` 的同名方法，是不同的东西）。

> **AUD-33 落地说明（2026-09-22）**：legacy HTML 已删除 —— AUD-18 分阶段退役的
> 第 ③ 阶段，前置 AUD-32（SPA 部署）已完成。
>
> **删除清单（全部落地）**：
>
> | 类别 | 内容 |
> |---|---|
> | 文件 | `cmd/analysis/static/` 6 个（5 html + 1 css，124K），目录一并删掉 |
> | 路由 | `main.go` 的 `router.Static("/static")` + 10 条 HTML 路由（`/`、`/index.html`、`/screen(.html)`、`/dashboard(.html)`、`/copilot(.html)`、`/strategy-selector(.html)`） |
> | 裸镜像 | `handlers_proxy.go` 的 4 条（`GET /ohlcv/:symbol`、`POST /screen`、`GET /stocks/count`、`GET /market/index`） |
> | 测试 | `deps_test.go` 的 `mustHave` 去掉 `GET /` 与 `GET /static/*filepath`；**新增 `mustNotHave`** |
> | 文档 | AGENTS.md §14、ARCHITECTURE.md（3 处）、deploy-config.md、ADR-011 尾部 Update 段 |
>
> **⚠️ 删除被写成了护栏，不是只删掉。** `deps_test.go` 新增 **15 条
> `mustNotHave`**（10 条 HTML 路由 + `/static/*filepath` + 4 条裸镜像），外加
> 4 条 `/api/` 前缀路由的**正向**断言。理由：只把 `mustHave` 里那两行删掉的话，
> 将来有人重新加一个 catch-all `/`，两个前端就会在同一端口上悄悄分叉，而**没有
> 任何东西会抱怨**。
>
> **破坏验证三处，各只红该红的**：
>
> | 破坏 | 结果 |
> |---|---|
> | 把 legacy 的 `GET /` 加回去 | 红 —— `route "GET /" belongs to the retired legacy UI (AUD-33) and must not come back` |
> | 把裸镜像 `GET /stocks/count` 加回去 | 红 —— 同上（`route "GET /stocks/count" ...`） |
> | 删掉 `/api/stocks/count`（模拟「顺手清理整个 proxy 块」） | 红 —— `expected /api-prefixed data route "GET /api/stocks/count" to remain registered` |
>
> **顺带修正 SPEC.md 的 Data Proxies 段 —— 那一整段有 7 行是错的。**
> 4 条裸镜像（本次删的）+ **3 条 ODR-062 早已删掉的**（`POST /sync/calendar`、
> `POST /api/sync/calendar`、`GET /api/v1/trading/calendar`）。后 3 条属
> 「文档没跟着代码走」：ODR-062 S-C 已把它们作为死代码删除，SPEC 却一直留着。
> 顺手核实了一个容易误判的点：`/api/sync/*path` 通配符**不会**复活
> `/api/sync/calendar` —— data-service 只在裸路径 `/sync/calendar`
> （`cmd/data/main.go:112`）提供日历同步，不在 `/api/sync` 前缀下。
>
> **⚠️ 前置「SPA 已被实际使用过」未严格满足**：整栈自 AUD-32 之后没起来过
> （只有 postgres / redis / data-service 在跑，`web` 与 `analysis-service` 都没起）。
> 判断它不构成阻塞的理由：① SPA 镜像在 AUD-32 已实证可用（构建 + 深链回退 +
> 反代 URI 完整 + SSE 破坏验证全过）；② 两套 UI 都要靠一个跑起来的
> analysis-service + 有数据的库才有意义，而库目前是空的（卡在 `TUSHARE_TOKEN`）
> —— 也就是说此刻**两套 UI 对若曦的实际可用性都接近零**；③ 删除是 git 可逆的。
> **若曦下次起整栈时，界面走 `http://localhost:8080`（不再是 `:8085`）** ——
> `:8085/` 返回 404 是预期行为，不是故障。

> **AUD-34 裁决（2026-09-22）**：若曦裁定 **保留 `LiveEngine`**，**本次不动代码**。
>
> 明确接受的状态：`LiveEngine`（`pkg/live/engine.go`，约 450 行的完整
> Start/Stop/SubmitOrder/PositionManager 编排）**生产零调用方**，只被测试覆盖
> （`live_test.go` / `engine_test.go`，含 AUD-16 新加的三例）。活的
> `/api/execution/*` 走的是 `MockTrader`（`live.LiveTrader` 的另一个实现），
> 不经过它。`SimulatedBroker` 同样零调用方。
>
> **⚠️ 这条裁决意味着一个已知风险被接受，不是被消除。** 登记的验收条件写的是
> 「要么被真实请求路径跑过至少一次，要么删除并写明理由；**不允许长期停在
> 『只有测试在用』**」—— 保留属于**第三条路**，所以它被记进 AGENTS.md §14 的
> 已知问题表，而不是当作已解决。
>
> 风险的形状：**一个从没被真实请求执行过的路径，等于把首次运行留给生产**。
> AUD-16 修的那个 bug 就是这类 —— `GetPositions()` 返回值拷贝导致
> mark-to-market 算了就扔，三个字段恒为 0，而它在「功能是死的」模块里躺了很久，
> 是逐行读才发现的，测试全绿也看不出来。
>
> **接线到实时行情 / 真实券商之前，第一件事是补一条从 HTTP 层真的接一次的
> 集成测试**，而不是直接上资金。这条已写进 AGENTS.md §14 的 Workaround 列。

> **AUD-27 落地说明（2026-09-22）**：`.gitattributes` 落地 + 工作区 EOL 归一化
> + 清掉 32 个未格式化文件 + CI 加 `gofmt` 门禁。
>
> **⚠️ 登记的实测是错的，落地前先重验。** 登记写「仓库 blob 里存的是 CRLF
> （实测 `git show HEAD:pkg/risk/lot.go` 有 99 行带 CR）」—— 实测**不成立**：
>
> | 检查 | 结果 |
> |---|---|
> | `git ls-files --eol '*.go'` | **558/558 全部 `i/lf`**（index 侧就是 LF） |
> | `git cat-file blob HEAD:<任一 .go>` | **零个含 CR** |
> | `git ls-files --eol`（全仓 915 文件） | 911 `i/lf` + 4 `i/none`（空文件/无尾换行） |
>
> 真相：**blob 本来就是 LF**；CRLF 只存在于**工作区**，来源是本机
> `core.autocrlf=true`（`PortableGit/.../etc/gitconfig`，**系统级** —— 所以
> `git config --global --get` 查不到，要用 `--list --show-origin`）。
>
> **推论**：登记说「Linux CI 与 Windows 本机都会列出全部 Go 文件」只对了后半句。
> Linux runner 上 checkout 本来就是 LF —— 也就是说**这个门禁在 CI 里一直是有效的**，
> 真正挡住它的是「本机跑不出可信结果，于是没人信它」（AUD-12 因此没加）。
>
> **顺带挖出被掩盖的债**：`gofmt -l .` 今天列 **429** 个。归一化 EOL 后仍列
> **32** 个 —— 说明 CRLF 噪声**额外掩盖了 15 个**真未格式化文件（另 17 个本来就
> 是 LF 所以一直可见）。错型：import 分组错序（`github.com/...` 插在 stdlib 组里）、
> struct 字段/字面量对齐、缺尾换行、**1 个文件带 UTF-8 BOM**
> （`pkg/tools/builtin/research_tool_test.go`）。
>
> **修法与实证**：
>
> | 步骤 | 做法 | 实证 |
> |---|---|---|
> | ① 加 `.gitattributes` | `* text=auto eol=lf`（仓库无 `.bat/.cmd/.ps1`，无需例外） | `git add --renormalize .` **暂存零个改动** —— 登记担心的「一次性大 diff」**不成立** |
> | ② 归一化工作区 | 713 个已跟踪文件 CRLF→LF（原地，逐文件 sha256 校验归一化后内容一致） | 归一化后 `git diff --quiet` **退出码 0**（零内容差异）；`git status` 干净 |
> | ③ 修 32 个真问题 | `gofmt -w` | `gofmt -l .` → **0**；diff 仅 32 文件 / 54+ 51- |
> | ④ CI 门禁 | go job 加 `gofmt` 步骤（Build / Vet / **gofmt** / Test） | YAML 解析通过；本地模拟步骤逻辑 → PASS |
>
> **⚠️ 中间踩到一个假象**：归一化后 `git status` 一度报 **713 个 ` M`**，但
> `git diff` / `git diff --numstat` / `git diff --ignore-cr-at-eol` **全为空**。
> 那是 **index stat-cache 未刷新**的幻影（mtime 变了、内容没变），
> `git add --renormalize .` 刷新索引后即消失。**判断「有没有真改动」要信
> `git diff --quiet` 的退出码，不要信 `git status` 的行数。**
>
> **破坏验证**（CI 步骤的 shell 逻辑，本地模拟）：① 新建一个故意未格式化的
> `.go` 文件 → 变红并列出该文件；② 往已格式化文件插入错序 import → 变红。
> 两次都只红该红的，删除/还原后回到 0。
>
> **验证**：`go build` / `go vet` / `go test ./... -count=1` 全绿；三道护栏全绿。
>
> **⚠️ 一处未处理（不在本项范围）**：`e2e/package.json`、`e2e/tsconfig.json`、
> `web/tsconfig.json` 三个 JSON **无尾换行**（`git ls-files --eol` 报 `i/none`）。
> 不影响 gofmt，另立议题。

> **AUD-30 落地说明（2026-09-22）**：`docs/SPEC.md` 的顶层定位从 **ADR-022**
> （Proposed 期间即被取代、**从未实施**）切到 **ADR-023 / ADR-024**。纯文档改动，
> 零代码。
>
> **改动清单**：
>
> | 位置 | 改法 |
> |---|---|
> | 头部 | `Version: 1.5.0 (Unified Research Platform — ADR-022)` → **`1.6.0 (AI 实验员实验室 — ADR-023 / ADR-024)`**；`Last Updated` 2026-09-15 → 2026-09-22；补 `Changelog v1.6.0` |
> | 核心章节 | `## Unified Research Platform (ADR-022, Proposed)` → **`## AI 实验员实验室（ADR-023 / ADR-024）`** —— 四层 L0-L3 / 双对等工作面 / 飞轮闭环 → 三角色 + 三层（**L1 数据 / L2 能力 / L3 AI 编排**）+ 验证器链（5 确定性 + 1 因果）+ 信息隔离 + EquityDeep 降为数据底座 + 可校准目标 + 数据归属 A-E + 证据服务 + **§策略执行载体（ADR-024）** |
> | §6 末 | `### 6. AI Research Service (port 8086)` **整块（~190 行）换成墓碑** —— 该服务 2026-09-18 已删（P2-5），SPEC 却仍标 `✅ Implemented` 并列约 100 行端点 |
> | §6 | Data Synchronization 子节上移 —— **SSE 路径是 `GET /api/sync/jobs/:id/progress`**，不是旧文档写的 `/api/sync/stream` |
> | Pipeline Stages | 第 3~5 步按 ADR-024 校准：LLM 生成的 Go 代码是 **artifact**（失败不阻断），回测跑的是**表达式信号** |
> | Phase 4 Changes | 加历史标注 —— 那张表是 Phase 4 的**历史交付清单**，不是现状 |
> | 3 处正文引用 | 重指 ADR-022 → ADR-023（external-source 段、`/api/factors/:factor_name`、`POST /api/datasource/switch` 退役说明） |
> | 层号警告 | 显式提示：ADR-022 用 `L0 = 数据面`，ADR-023 用 **`L1 = 数据层`** —— 旧任务号里的「L0」一律读作 **L1 数据层** |
>
> **⚠️ 实际比登记写的严重**：登记说「全文 7 处引用 ADR-022」，落地时另发现三处
> 独立问题 —— ① 已删除的 `:8086` 服务被标为 `✅ Implemented`；② 节号 `6.` 重复；
> ③ Batch / Walk-Forward 路径写错（文档写 `/api/batch/backtest`，实际是 `/api/batch`）。
> 三处都会让读者照着行动，属「按文档做会做错」，不是措辞问题。
>
> **验证**：`go build` / `go vet` / `go test ./... -count=1` 全绿；三道护栏
> （`check_doc_links.py`、`--include-archive`、`check_deploy_consistency.py`）全绿。
>
> **顺带修正 `docs/ADR.md` 尾注累计块**：核对验收标准第 3 条（`docs/ADR.md` 与
> AGENTS.md §11 对 SPEC 的描述一致）时发现 —— ADR.md 的 **ADR/ODR 索引表本身是
> 对的**（ADR-022 已标 `Superseded by ADR-023`；ODR-065 已录入），但尾注的
> 「累计」块陈旧：写「ADR 累计 22 条 … **ADR-021 由 ADR-022 取代**」—— 而
> **ADR-022 本身早已被 ADR-023 取代**（这正是本项的正面主题）。已按实测改正为
> **ADR 24 条 / ODR 65 条**，取代链补上 ADR-023。**这不是新问题** —— ODR-064
> 那轮也修过同类的「尾注 vs 索引表」漂移。AGENTS.md §11 对 SPEC 的描述是中性的
> （「技术规格、API 定义、数据模型、Strategy 接口」），不含 ADR-022 定位，
> **无需改**。
>
> **有意不改**：`Owner: 龙少 (Longshao)` 是 `SPEC.md` / `ADR.md` / `TEST.md` 三个
> 文件的一致约定（文档署名），属独立议题，本次不动。

> **AUD-28 落地说明（2026-09-22）**：把 AUD-12 的先例推广到其余 3 个包 ——
> `internal/httpserver`、`cmd/data`、`pkg/api` 各新增 `main_test.go` 的
> `TestMain`（`gin.SetMode(gin.TestMode)`），删掉全部逐测试 / 逐 helper 的调用。
>
> **登记是快照，不是全貌（第 3 次了）**：登记列了 5 个文件，取证实测到 **6 个** ——
> `cmd/data/handlers_factor_test.go` 的 `factorRouter()` 也调 `gin.SetMode`，
> 不在清单里。全仓调用点共 **11 处**：6 个测试文件（10 处）+ 3 处生产代码 +
> 2 处已合规的 `TestMain`。
>
> **`pkg/api` 的登记前提是错的**：它本来就用 `func init() { gin.SetMode(...) }`
> 集中设置（该包只有 1 个调用点，且在任何测试 goroutine 之前执行）——
> **本来就无竞争**，登记说它「测试各自调 `SetMode`」不成立。仍改成 `TestMain`，
> 理由是 `init()` **不可 grep**：读者在测试文件里看到 `gin.SetMode` 无法判断这个
> 包是集中设置还是逐测试调用。现在 `grep -rn "func TestMain" <pkg>/` 就能回答。
>
> **哪些测试加了 `t.Parallel()`**（跟着 `pkg/auth` 的先例一起留下，不是验证完就删
> —— 没有并行测试，逐测试 `SetMode` 永远不会被 `-race` 看见）：
> `internal/httpserver` 的 11 条（errors / cors 全部）、`cmd/data` 的 4 条
> （handlers_factor 2 条 + handlers_ingest 2 条）。
> **故意不加的**：
> - `cmd/data/setup_test.go` 全部 —— 它们驱动**全局 viper**（`loadConfig()` 写
>   默认值、`TestBuildRouter_WiresCORSAllowlist` 还 `viper.Set`），并行会把 gin
>   的竞争换成 viper 的竞争，等于用一种竞争换另一种。
> - `internal/httpserver/cors_test.go` 的 `TestAllowedOrigins_Parsing` —— 它用
>   `t.Setenv`，与 `t.Parallel` 互斥（testing 直接 panic）。
>
> **护栏两向实证**（本机无 gcc，`-race` 走 docker + `golang:1.25`）：
> - 修复后 `go test -count=1 -race ./internal/httpserver/... ./cmd/data/...
>   ./pkg/api/...` → 全绿。
> - **破坏验证**：把 `gin.SetMode(gin.TestMode)` 塞回 `newRecorder()`（即 AUD-28
>   之前的样子）→ `-race` **变红**，栈正是登记预测的那一对 ——
>   写 = `gin.SetMode`（`errors_test.go:17`）← `TestWrap_PrefixPlusLoggedCause`；
>   读 = `gin.IsDebugging` ← `debugPrintWARNINGNew` ← `gin.New()` ←
>   `TestCORS_AllowedOriginEchoed`。恢复后全绿。
> - 这条同时证明登记的前提成立：**「潜在雷」不是猜测** —— 两个并行测试各自建一次
>   router 就够撞上，不需要任何额外条件。
>
> **验证**：`go build` / `go vet` / `go test ./... -count=1` 全绿（72 包）；
> 三道护栏全绿。

> **AUD-29 落地说明（2026-09-22）**：gin 的运行模式改为**显式配置项 + 启动期设一次**。
>
> **原来是什么样**：三处生产代码在 `buildRouter` 里按日志配置**推断** gin mode，
> 三个口径还不一样 —— analysis 看 `logging.format == "json"`、data 看
> `logging.level != "debug"`、strategy 看 `config.Logging.Level != "debug"`。
> 同一份配置能得出不同结果：`level: debug` + `format: json` → analysis 进 release、
> data 进 debug。
>
> **改法**：新增 `internal/httpserver` 的 `ConfigKeyGinMode = "server.gin_mode"` +
> `ApplyGinMode()`；三个服务在启动早期（`buildRouter` 之前）各调一次；三处运行期
> `gin.SetMode` 全部删除。取值 `debug | release | test`，未配置 / 不认识一律
> **release**（fail-safe：gin 自己的默认是 debug，一个笔误不该让线上进程开始打
> 路由表）。三份 `config/*.yaml` 显式写 `gin_mode: "release"` —— 与改动前三个服务
> 的实际生效值一致，**行为中性**。**不写 `SetDefault`**：默认值只留在
> `ginModeFor` 的 `case ""` 一处，少一个漂移源。
>
> **取证时发现两个登记里没有的问题**：
>
> ① **`GIN_MODE` 是同一个决定的隐藏入口**：`deploy/k8s/configmap.yaml` 一直写着
> `GIN_MODE: "release"`，而且**真的生效** —— gin 自己在 `init()` 里读这个 env
> （`gin/mode.go:52`）。但全仓 grep 不到任何读取点，只有 gin 的内部实现知道它存在；
> 而且只有 k8s 有（compose 没写）→ **两条部署路径的 gin mode 来源不同却不报错**
> （与 AUD-32 第 9 例同型）。已从 configmap 移除，并把这条不变量写进
> `tools/check_deploy_consistency.py` 的**检查 4**：部署配置里出现 `GIN_MODE` 即
> 报错。`config/` 本来就被各 Dockerfile `COPY` 进镜像，所以 k8s 读到的仍是同一份
> 文件，移除后行为不变。
>
> ② **`cmd/strategy` 没有任何 env 覆盖**：它的 `loadConfig` 没接
> `viper.AutomaticEnv()`（analysis / data 都接了）。我一开始在
> `config/strategy-service.yaml` 里照抄了「env 覆盖：SERVER_GIN_MODE」，**写完复核
> 读取路径才发现是假的** —— 已改成如实说明。这是「配置里的注释陈述被否掉的方案」
> 的又一例（同 AUD-32 第 9 例），另立 **AUD-36**。
>
> **顺带发现（另立 AUD-35）**：`LOG_LEVEL` / `LOG_FORMAT` 是**死配置** —— compose
> 里 3 处、k8s configmap 里 2 个键，全仓无人读；viper AutomaticEnv 的键名是
> `logging.level` → `LOGGING_LEVEL`，`LOG_LEVEL` 永远匹配不上。
>
> **护栏两向实证**：
> - **结构性护栏** `TestGinSetModeOnlyInSanctionedPlaces`：解析 **AST**，断言
>   `gin.SetMode` 的**调用**只出现在 `internal/httpserver/ginmode.go`，或
>   `_test.go` 里 `TestMain` 的函数体内。
> - **行为护栏**：`cmd/data` 与 `cmd/analysis` 各一条
>   `TestBuildRouter_DoesNotTouchGinMode` —— 用「旧代码会因此切 ReleaseMode」的
>   那份配置调 `buildRouter`，断言 `gin.Mode()` 没变。
> - **破坏验证**：把旧代码那行加回 `cmd/data/setup.go` 的 `buildRouter` →
>   结构性护栏报 `cmd/data/setup.go:240`、行为护栏报 `expected "test" / actual
>   "release"`；在非 `TestMain` 的测试函数里插一行 → 结构性护栏报
>   `internal/httpserver/errors_test.go:104`。两处恢复后全绿。
> - **部署护栏破坏验证**：把 `GIN_MODE` 加回 configmap → 检查 4 报错（exit 1）；
>   移除后 exit 0。
>
> **⚠️ 护栏第一版是错的（值得记）**：第一版按**文本**扫 `gin.SetMode(`，结果被
> 自己文档注释里的引文误报 —— `cmd/analysis/middleware_test.go` 与
> `cmd/data/setup_test.go` 的新注释里引用了旧代码那行。**字符串护栏分不清「调用」
> 和「提到」**；改成解析 AST 后精确。这个失败本身有价值：会误报的护栏，下一个人
> 就会把它关掉。
>
> **验证**：`go build` / `go vet` / `go test ./... -count=1` 全绿；`-race`
> （docker + `golang:1.25`）四个受影响包全绿；三道护栏全绿。

> **AUD-20 落地说明（2026-09-22）**：若曦裁决 **做日期分段**。`pkg/fees` 新增
> `DefaultStampTaxRateBefore = 0.001`、`StampTaxCutDate = 2023-08-28`、
> `StampTaxRateFor(asOf, current, before)` —— 零 `asOf` 取当前值、非正覆盖值回退到常量，
> 与 AUD-07 的 `resolvePriceLimit` **同形**（那个函数的注释早就点名了 AUD-20，
> 设计一直在等）。`Tracker.feeSchedule()` → `feeSchedule(asOf)`，5 个调用点全部传当天
> 日期；`TradingConfig` 加 `stamp_tax_rate_before`（`pkg/backtest/aliases.go` 双别名，
> 同 AUD-06 先例）。
>
> **误差方向是这条的要害**：拿减半后的 0.05% 去算减半前的卖出 = **低估成本 → 高估收益**，
> 与「校准优先」直接冲突。
>
> **三重破坏验证**：① `feeSchedule` 忽略 `asOf` → 只有 tracker 的两条日期分段测试变红；
> ② 解析器改 `if false` → fees 两条 + tracker 两条变红；③ 两个常量合并为 0.0005 →
> `TestStampTaxRate_HistoricalTimeline` + `TestStampTaxRate_Fractions` 变红。
> 另把 `ashare_test.go` 里 `const pre2023CutRate = 0.001` 改为引用常量，**消掉两处独立 pin**
> （否则改常量时测试不会跟着动）。
>
> **为什么不给 yaml 加键**：引擎根本不读顶层 `trading:` —— 加了也是死键，见 AUD-37。

> **AUD-21 落地说明（2026-09-22）**：若曦裁决 **板块感知 + 单一权威**。
> `pkg/risk.ValidateOrderQuantity(shares, symbol, isSell)` 成为唯一的申报量校验，
> **复用 `lot.go`（AUD-09）那张板块表**；xtp 的 `int(quantity)%100 != 0` 改为调它。
> 顺带修掉同一行里的两个附带缺陷：`int()` **截断**（100.9 股曾能通过这道门）与
> **假文案**「A-share quantity must be multiple of 100 (1 lot)」—— 它不是 A 股规则，
> 只是主板/创业板规则。卖侧一律放行：零股卖出规则需要持仓状态，交给柜台。
>
> **两重破坏验证**：① 恢复旧 `%100` → 只有科创板/北交所/卖侧用例变红，主板用例仍绿
> （证明护栏有区分度，不是「全都拒」）；② 把科创板分支误用主板规则 →
> **属性测试**自行抓出（`NormalizeOrderQuantity` 的输出必须全部通过校验，
> 11 符号 × 16 输入），无需手写用例。
>
> **未实现（登记外发现，已记在 `lot.go` 注释）**：单笔申报上限 —— 沪深主板 100 万股 /
> 创业板限价 30 万·市价 15 万 / 科创板限价 10 万·市价 5 万 / 北交所 100 万股。
> 未建模。

> **AUD-22 落地说明（2026-09-22）**：若曦裁决 **只做回测侧** —— live 侧不接，
> 因为 `pkg/live/broker/xtp` 是**零 import 孤岛**（与 AUD-34 同族），接了就是死代码。
>
> `pkg/marketdata` 成为 ST 判定与上限的**唯一权威**（新文件 `riskwarning.go`）：
> `IsRiskWarningName` 从 `pkg/backtest/pricelimit.go` 下移（AUD-08 的实现原样搬，
> backtest 侧留薄包装以免 churn 调用点）；`RiskWarningDailyBuyCap(symbol)` 按板块给上限
> —— 沪深/创业板 50 万、北交所 20 万、**科创板 0（豁免）**、unknown 取较严的 20 万。
> tracker 加 `stockNames` 表 + 当日累计状态（`dailyRWBuy` / `dailyRWBuyDay`），
> `engine.go` 在 `fetchMarketDataForDay` 之后**每日刷新**该表 —— ST 身份会变，
> 一次性表等于把今天的 ST 名单套到 2015 年的回测上。
>
> **⚠️ 关键勘察：生产路径不是 `ExecuteTrade`。** `NewEngine` **无条件**
> `executionBridge.Set(executionService)`，于是 `useExecutionService` 对非 Hold 方向
> **恒真** → 买入走 `executeViaExecutionService` → **`Tracker.ApplyTrade`**。
> 只在 `ExecuteTrade` 加护栏 = **单测全绿、生产空转**。故两个入口都接。
>
> **破坏验证（三重，并因此发现一处真缺口）**：
>
> - 删掉 `engine.go` 的 `SetStockNames` 调用 → AST 结构护栏变红，**只有它**变红。
>   **AST vs 文本的取证**：该文件里 `"SetStockNames"` 共出现 **3 行**（2 处注释提及
>   + 1 处真实调用）。注释提及保留、只删调用后，文本 grep 仍命中 **2 行**，
>   而 AST 护栏准确报 **0 个调用点** —— 这才是「护栏没被提及骗到」的证据。
>   ⚠️ **这条说法最初是编造的**：第一版落地说明写「注释里出现 3 次」时，该文件里
>   其实**只有 1 行**（就是调用本身），注释根本没提到它 —— 是写记录时凭印象补的
>   细节，事后 grep 复核才发现。已把注释改成**真的**提到该函数（本来也该提，
>   读者一眼能对上），并**重做破坏**验证：提及 2 处 + 调用 0 处 → AST 报 0、
>   文本命中 2。**取证细节必须当场 grep 复核，不能凭印象写**（PITFALLS §31）。
> - 摘掉 `ApplyTrade` 的 cap 检查 → 生产路径护栏变红，但 **tracker 包整体全绿**。
>   这暴露了一个真问题：tracker 自己的 8 条测试**只走 `ExecuteTrade`**，
>   对 `ApplyTrade` 完全盲。已补 `TestTracker_RiskWarningDailyBuyCap_ApplyTradePath`，
>   重跑破坏后它**唯一变红** —— 盲区补上，不是「加了一条重复测试」。
> - 去掉科创板豁免 → 只有 STAR 相关用例变红（`marketdata` 2 条 + `tracker` 1 条），
>   主板/北交所/重置日保持绿 —— 爆炸半径精确。
>
> **登记有误（登记缩小了范围）**：原登记只写北交所 20 万股、把沪深列为「需一并查证」。
> 查证结果：**沪深也有，而且是 50 万股，已施行多年**（沪 4.4.10 / 深 4.5.4）。
> 三所口径一致：委托买入 + 当日已买入 + 已申报未成交未撤销 ≤ 上限，**普通账户与
> 信用账户合并计算**；例外为上市公司回购、5% 以上股东按已披露增持计划增持。
> **科创板豁免**依据沪 6.14（科创板 ST **不进**风险警示板）。
>
> **未做**：live 侧（`order_manager`）；**限价委托要求**（沪 4.4.9：买卖风险警示股票与
> 退市整理股票应当采用限价委托）未建模，已记在 `riskwarning.go` 头注释。
>
> **顺带发现 → 新登记 AUD-37**：引擎的 `TradingConfig` 从不读 yaml 的 `trading:` 块。

> **AUD-20 / 21 / 22 共同验证（2026-09-22）**：`gofmt -l .` **0 文件**、
> `go build ./...` / `go vet ./...` / `go test ./... -count=1` **全绿（72 包）**；
> `-race`（docker + `golang:1.25`）覆盖 `pkg/backtest/...` / `pkg/marketdata` /
> `pkg/risk` / `pkg/fees` 共 **17 包全绿**；两道护栏脚本
> （`check_doc_links.py` / `check_deploy_consistency.py`）全绿。

> **AUD-26 落地说明（2026-09-22）**：若曦裁决 **`os.Executable()` 自举**。
> 8 个测试全部改为重入测试二进制自身；模式走**环境变量**而不是 argv（argv 正是
> 其中几个测试要断言的东西）。`TestMain` 在框架启动前拦截 helper 模式，所以子进程
> 的 stdout 里不会混进框架的 "PASS"。
> - **登记漏列 3 个测试**（登记写 5 个用宿主命令，实际 8 个；另
>   `limits_windows_test.go` 还有第 9 处 `echo`）。已一并改掉。
> - **`Options.Stdin` 此前零测试**：旧 `TestRun_StdinAndEnv` 名字承诺了 stdin，
>   函数体从没设过它。已拆成 env / stdin / stdin 默认 EOF 三条。
> - `TestRun_Dir` 不再在 Windows 上 `t.Skip`（跳过 = 让 `Options.Dir` 在最需要它的
>   平台零测试）；`TestRun_BinaryNotFound` 不再用 `/nonexistent/binary`（在 Windows
>   上读起来像 UNC 路径，测的是另一件事）。
> - **两条新护栏**：`TestCrossPlatformTestsNameNoHostCommand`（**AST** 结构护栏：
>   `runner_test.go` 与 `limits_windows_test.go` 里任何 `Run` / `RunExitCode` /
>   `exec.Command` 都**不许出现字符串字面量命令名**。规则是「不许字面量」而不是
>   「必须是 `helperBinary(t)`」，因为 `TestRun_BinaryNotFound` 合法地传变量）、
>   `TestHelperRefusesToRecurseWithoutAMode`（防递归护栏的护栏）。
> - **防递归护栏本身是必要的**：子进程若用 `Options.Env == nil` 会继承 suite 标记
>   → 跑整套测试 → 再开子进程 = fork 炸弹。`TestMain` 里拦截并 exit 97。
> - **破坏验证（3 次）**：① 注入 `_ = exec.Command("echo")`（行为中性）→ **只有 AST
>   护栏红**、全部行为测试绿（证明护栏有独立检出力）；② `TestRun_ExitZero` 改回
>   `"echo"` → 护栏红并指名 `runner_test.go:184:25`，而**该测试本身仍绿**（本机装了
>   Git for Windows）—— **AUD-26 的失败模式自己复现了一遍**；③ `TestMain` 的 guard
>   改成 exit 96 且无消息 → 防递归测试两条断言都红。**注**：把 guard 整块删掉**不能
>   实跑**（它本身就是那个 fork 炸弹），故改为钉住 guard 的可观测契约。
> - **行为证据**：把 `PATH` 指向空目录后 `go test ./internal/sandbox/runner/` 仍全绿。
> - `-race`（docker + `golang:1.25`）18 包全绿。

> **AUD-25 落地说明（2026-09-22）**：若曦裁决 **探测 shell 能力，缺失时报
> `ErrLimitsUnsupported`**。wrapper 现在对**每个**要用的 ulimit 先探测再加：
> `ulimit -u >/dev/null 2>&1 || { echo '<不支持标记>' >&2; exit 126; }` 然后
> `ulimit -u 64 || { echo '<设置失败标记>' >&2; exit 125; }`。
> - 两个标记 + 两个状态码，`Run` 里分别映射到 `ErrLimitsUnsupported` /
>   `ErrLimitSetupFailed`。**126 也是任何程序都能用的合法退出码**，所以和 125 一样
>   要求「状态码 AND 标记」双匹配（已补对称护栏 `TestRun_Exit126WithoutMarker…`）。
> - 探测是**逐 limit** 而不是只给 `-u`：「这个 shell 表达不了 RLIMIT_AS」比「这个值
>   被拒了」更准确，代价是一次 builtin 调用。
> - **如实记录的局限**：`WithAllowUnenforcedLimits` **救不了**这一条 —— 那个决定发生
>   在子进程里，父进程早已做完选择。dash 系统上 `NumProcs` 请求会 fail-closed，唯一
>   的逃生阀是「不要请求这个限额」。已写在错误与选项两处文档里。
> - **实证**：`golang:1.25`（`/bin/sh -> dash`）走不支持分支；`golang:1.25-alpine`
>   （busybox ash）走成功分支并打印 `64`。**两侧都断言、都不跳过**，所以测试不依赖
>   跑它的镜像是哪个。另有**对照组**：同一份脚本分别跑在 dash 与 bash 下（bash 那半
>   是承重的 —— 没有它，一个永远 exit 126 的脚本也能通过 dash 那半）。
> - **目标用 shell builtin 而不是 Go 程序**：RLIMIT_NPROC 是按**用户的总进程数**算
>   的，任何需要 clone 线程的 Go 子进程在低限额下都可能起不来；builtin 不 fork。
> - **破坏验证（3 次）**：① 去掉探测 → 4 条护栏红（脚本形状 ×2 / 顺序 / 端到端）；
>   ② 探测保留但用错标记 → 同样 4 条红（**标记本身也是承重的**）；③ 探测与设置对调
>   → 顺序测试以 `RLIMIT_CPU: the capability probe must come first` 变红。
> - **生产侧是潜伏而非现患**：组合根请求 `{1GiB, 25s, 256 fds}` **不含 `NumProcs`**，
>   且运行镜像是 alpine（busybox ash 有 `-u`）。

> **AUD-24 落地说明（2026-09-22）**：若曦裁决 **做可映射子集 + 明确报告不支持项**。
> **登记有两处错，两处都订正而非绕过**：
> - ① 登记说「Job Object 需要 `CREATE_SUSPENDED` 才能在 exec 前挂载」—— **做不到**。
>   `syscall.StartProcess` 在返回前就关掉了主线程句柄（`sdk/go1.25.0/src/syscall/
>   exec_windows.go` 里 `defer CloseHandle(Handle(pi.Thread))`），没有线程可
>   ResumeThread；`syscall.SysProcAttr` 也没有 `PROC_THREAD_ATTRIBUTE_JOB_LIST`，
>   构造不出 `STARTUPINFOEX`。所以**只能在 `Start()` 之后挂载**，子进程有一小段无
>   约束窗口。已把窗口写进 `attachProcessLimits` 的注释，含两条后果：「窗口内跑完的
>   子进程从未被限额」与「窗口内生出的孙进程不在 job 里」。
> - ② 登记说验收标准是「Windows 上不需要逃生阀即可构建」—— **构造上达不到**。
>   Job Object **没有**句柄数限额、也**没有**单文件大小限额（逐字段核对
>   `JOBOBJECT_BASIC_LIMIT_INFORMATION` / `JOBOBJECT_EXTENDED_LIMIT_INFORMATION`
>   与整个 API）。验收标准改写为「可映射子集真的生效，其余被点名」。
> - 映射：`CPUSeconds → JOB_OBJECT_LIMIT_PROCESS_TIME`（100ns）、`MemoryBytes →
>   PROCESS_MEMORY`（提交内存）、`NumProcs → ACTIVE_PROCESS`（**语义不同**：POSIX
>   按用户算，这里按 job 算），外加 `KILL_ON_JOB_CLOSE`（失控子进程活不过守护进程）。
> - **`WithOnUnenforcedLimits` 回调签名改了**：从 `func(argv []string)` 改为
>   `func(argv []string, unenforced Limits)`。理由：「某个限额没生效」本身不可行动，
>   运维要知道**是哪个**。`cmd/analysis` 把它记成一个日志字段。
> - **实测（不是推断）：`JOB_OBJECT_LIMIT_PROCESS_TIME` 的触发点几乎不随限额变化。**
>   0.1s / 1s / 3s 三个限额都在 **5.3–7.2s 累计用户时间**才杀掉一个 8s 的 CPU 燃烧；
>   而**同一燃烧不设限额时跑满 8.1s 正常退出**（对照组）。所以 Windows 上的
>   `CPUSeconds` 是**兜底**，不是「最多 N CPU 秒」的紧界 —— 已写进包级威胁模型，
>   免得有人拿它做任务预算。
> - **验证**：① **回读式单元测试** —— `createJobObject` 的结果用
>   `QueryInformationJobObject` 读回来断言，而不是相信刚填进去的结构体（flag 位、
>   100ns 换算、内存/进程数字恰是「看着对其实错」的高发区）；另一条钉住
>   `OpenFiles` / `FileSize` **不设任何 flag**。② **每个可映射字段一条行为测试，
>   各带对照组**：内存（512MiB 在 128MiB job 里死、在 2GiB job 里活）、活跃进程数
>   （无限额能 spawn、限额 1 时被拒）、CPU 秒（见上面的松紧度；因要烧核数秒，用
>   `testing.Short()` 门控）。③ **破坏验证 4 次**：只建不挂 → 3 条行为测试红（CPU
>   那条跑满 12s 才红，正说明限额是死因）；`unenforceableOnWindows` 返回零 → 5 条红
>   （含跨平台契约 `TestRun_LimitsAreEnforcedOrRefused`），即「静默丢限额」这个 bug
>   复现；去掉 `KILL_ON_JOB_CLOSE` → 2 条单元测试红；100ns 乘数写错 10000 倍 → 单元
>   测试以 `expected: 250000000, actual: 25000` 指名单位变红。
> - `golang.org/x/sys` 从 indirect 提为 direct（版本不变，v0.46.0 本就在依赖图里）。

> **AUD-38 落地说明（2026-09-22，登记外发现）**：CI 是 Linux-only，全仓 2 个
> `//go:build windows` 文件（`rlimit_windows.go` / `limits_windows_test.go`）
> **从不被 CI 编译** —— 而这两个文件是 Windows 上唯一的限额强制路径（AUD-24）。
> 用 `vet` 而不是 `build`：`go build` **不编译 `_test.go`**，而 AUD-26 恰在这两个
> 测试文件里引入过编译错误、直到在 Windows 上跑才暴露。**破坏验证（两侧）**：往
> `rlimit_windows.go` 注入类型错误 → 旧门禁 `go vet ./...`（Linux）**绿**（完全
> 漏检）、新门禁 `GOOS=windows go vet ./...` **红**并指名
> `rlimit_windows.go:14:13`。

> **AUD-24 / 25 / 26 / 38 共同验证（2026-09-22）**：`gofmt -l` 0 文件；Windows 侧
> `go build` / `go vet` / `go test ./... -count=1` 全绿；Linux 侧（docker +
> `golang:1.25`）`go build` / `go vet` / `go test -count=1` 全绿，且
> `internal/sandbox/...` + `cmd/analysis` 在 `-race` 下全绿；`GOOS=windows go vet
> ./...` 从 Linux 容器可跑且通过。四个提交：`c5adcab`（AUD-26）/ `384c26b`
> （AUD-25）/ `aff1028`（AUD-24）/ `c4dd2bd`（AUD-38）。

> **登记缺口（2026-09-21 复核时发现）**：ODR-065 的 24 项里有 3 项在登记环节掉了 ——
> M4（staticcheck 可绕过）、L2（live engine 组合状态，报告自标"未逐行复核"）、
> L3（legacy HTML 残留）。原表只有 AUD-14/AUD-15 两行却写"新增 3 项"，那第 3 项
> 是已被判定为误报的 AUD-L1。现补为 AUD-16/17/18，24 项全部有主。

> **AUD-35 落地说明（2026-09-22）**：若曦裁决 **改名**（不加 BindEnv 别名）。
> `docker-compose.yml` 3 处 `LOG_LEVEL` → `LOGGING_LEVEL`（data / strategy /
> analysis），`deploy/k8s/configmap.yaml` 的 `LOG_LEVEL` / `LOG_FORMAT` →
> `LOGGING_LEVEL` / `LOGGING_FORMAT`。**为什么不加别名**：AUD-29 移除 k8s
> `GIN_MODE` 时已确立「一个决定只留一个入口」（configmap 里那条注释就是理由），
> 加 `BindEnv("logging.level", "LOG_LEVEL")` 与先例相反，而且会让错名继续留在
> 部署文件里误导下一个人。**护栏（4 条，两向都钉）**：cmd/analysis 与 cmd/data
> 各一对 —— `LOGGING_LEVEL` / `LOGGING_FORMAT` 生效（正向）、`LOG_LEVEL` /
> `LOG_FORMAT` **不**生效（负向，「退役的名字不许回来」）。cmd/data 那份必须
> 单独写：它驱动的是**全局 viper 单例**，与 analysis 的独立 `viper.New()` 是两条
> 不同接线。**破坏验证**：① 把 `SetEnvKeyReplacer` 的 `"."` 改成 `"-"` → 只有
> 正向那条红（全包仅此一条）；② 把 `BindEnv("logging.level", "LOG_LEVEL")` 加回来
> → 只有负向那条红。提交 `3bb7e7d`。

> **AUD-36 落地说明（2026-09-22）**：若曦裁决 **接上 AutomaticEnv**，与另两服务
> 一致。`cmd/strategy/main.go` 的 `loadConfig` 加 `AutomaticEnv` +
> `SetEnvKeyReplacer`；新建 `cmd/strategy/main_test.go`（本包此前**零测试文件**，
> 这正是缺口能活下来的原因之一）；`config/strategy-service.yaml` 里那段
> 「⚠️ 本服务没有 env 覆盖 …… SERVER_GIN_MODE 在这里不生效」**已过期**，改为与
> 另两份 config 一致的「env 覆盖：SERVER_GIN_MODE」。
> **护栏的关键设计**：断言落在 **`Unmarshal` 之后的 `Config` 结构体**上，不是
> `viper.Get` —— `loadConfig` 走 `viper.Unmarshal`，而 env 只对「viper 已知的键」
> （来自配置文件或 `SetDefault`）才可见；只断言 `GetString` 会在 `Unmarshal` 静默
> 丢掉覆盖时依然通过，等于钉错了东西。**破坏验证**：移除 `AutomaticEnv()`（即修复
> 前的原状）→ 基线仍绿、7 条 env 断言全部指名失败 —— **这条护栏对修复前的代码是
> 红的**，即它真能抓住这个缺口，而不是恰好通过。提交 `df58af3`。

> **AUD-37 落地说明（2026-09-22）**：若曦裁决 **引擎改读顶层 `trading:`**。
> 落地时发现两件登记没写的事：
>
> ① **`backtest.trading` 那条路径今天确实活着** —— `engine_accessors_test.go` 与
> `engine_deterministic_replay_test.go` 两个夹具都在写它。核实过：两个文件里
> **没有任何**对 Trading / commission / tax 的断言，也没有 `config.Trading` 的直接
> 引用 —— 所以它们既没暴露问题、也没证明这条路径（夹具里的错路径是「哑」的）。
> 因此「引擎改读顶层」必须**同时**让旧路径退役（`Config.Trading` 的
> mapstructure tag 改成 `"-"`）：留着两条路径的话，顶层那条永远胜出（yaml 里总是
> 有它），`backtest.trading` 就变成「看着有效、实际被遮蔽」的第二个入口 —— 正是
> 本次要修的缺陷类型。
>
> ② **整体替换是地雷，且机制比登记写的更狠**：`portfolio.ComputeFees`
> （`portfolio.go:45-60`）**不调** `fees.AShareFees.ApplyDefaults`，所以
> `MinCommission` 为 0 = **没有最低佣金**、`TransferFeeRate` 为 0 = **不收过户费**
> —— 两者都让成交静默变便宜、回测收益被抬高。（`fees.StampTaxRateFor` 与
> `resolvePriceLimit` 则各自有逐字段回落，所以不是所有字段都同样危险。）故新增
> `TradingConfig.WithDefaults()` 逐字段回落。另补 `stamp_tax_rate_before: 0.001`
> 进 yaml（与 `price_limit` 的 `st` / `st_before` 同模式：当前值与历史值分开）。
>
> **护栏（pkg/backtest 5 条 + contracts 1 条）**：9 个键全部用**非默认值**驱动
> 逐字段断言（静默回落或接错键都会红）；只设一个键时其余字段必须是**默认值而非
> 0**；无 `trading:` 块的启动路径；「退役路径不许回来」；以及一条**结构性护栏**
> `TestTradingBlockInServiceConfigHasNoDeadKeys` —— **解析真实 yaml**（不是文本
> grep），沿结构体 `mapstructure` tag **递归**核对 `trading:` 块里每个键都有 Go
> 读取者，并显式白名单两个实盘专用键（`emergency_token` /
> `default_user_profile`）。**破坏验证（4 次）**：A 摘掉顶层读取 → 红 2 绿 3；
> B 换回整体替换 → **只红 1** 并指名「min_commission 不能变成 0」；C 把
> `backtest.trading` 标签加回来 → **只红 1**（退役路径那条）；D 往真实 yaml 注入
> 死键（**顶层 + 嵌套各一个**）→ 结构护栏报出 `trading.bogus_top_level_knob` 与
> `trading.price_limit.bogus_nested_knob` 两条完整路径 —— **嵌套那条同时证明递归
> 分支真的在跑**。四次破坏后均用备份恢复，`grep SABOTAGE` = 0、`diff` 与备份
> 一致、全量重跑绿。提交 `7fcf7d2`。

> **AUD-35 / 36 / 37 共同验证（2026-09-22）**：`gofmt -l`（本批改动文件）0 命中；
> `go build ./...` / `go vet ./...` / `GOOS=windows go vet ./...` 全绿；
> `go test ./... -count=1` **73 个包 ok、0 失败**；`check_doc_links.py` 52 文件无
> 坏链；`check_deploy_consistency.py` 四项全绿。三个提交：`3bb7e7d`（AUD-35）/
> `df58af3`（AUD-36）/ `7fcf7d2`（AUD-37）。

> **AUD-39 落地说明（2026-09-22）**：若曦裁决 **k8s 改用 `DATABASE_*` + `REDIS_URL`、
> 统一 `quant_trading` / `postgres`、compose 密码收敛为单一变量、`${...}` 占位符一并消灭**。
>
> **前提核实纠正了三处登记说错 / 漏掉的地方**（登记是审查时点快照，落地要按源码重扫）：
> ① 「compose 侧反而是对的」只对一半 —— 六项齐全的只有 analysis-service；
> data-service 只拿到 `DATABASE_PASSWORD` + `REDIS_URL`，`DATABASE_HOST/PORT/USER/
> DATABASE` 靠 `config/data-service.yaml` 的值兜底（P1-8 同型「碰巧能用」）。
> ② 登记漏了 **compose 侧的真 bug**：postgres 容器读 `${DB_PASSWORD:-postgres}`、应用读
> `${DATABASE_PASSWORD:-postgres}`，**两个独立变量**，默认值恰好相同所以能用，改成
> 非默认值（改哪个）就必然对不上；`ADR-017` 与 `SPEC.md` 用的是第三个口径 `DB_PASSWORD`。
> ③ 登记漏了 **值也不匹配**：configmap 是 `quantlab` / `quantlab`（StatefulSet 按此建库
> 建用户、探针也是 `-U quantlab -d quantlab`），应用读 `quant_trading` / `postgres`
> —— 即使键名与密码都修好，库名 / 用户名仍然对不上。另外登记写的 `engine.go:173`
> 实际是 **180**（行号漂移）。
>
> **改法**：`deploy/k8s/configmap.yaml` 只保留一份**应用词汇表**（`DATABASE_HOST/PORT/
> USER/DATABASE` + `REDIS_URL` + `DATA_SERVICE_URL` + `LOGGING_*`）；postgres 容器用
> `configMapKeyRef` 把 `DATABASE_DATABASE` / `DATABASE_USER` 映射到它认的
> `POSTGRES_DB` / `POSTGRES_USER` —— **一份词汇表，各自在边界上翻译**，不在同一处重复
> 定义第二个名字。两个应用 deployment 的 `POSTGRES_PASSWORD` 改名 `DATABASE_PASSWORD`；
> `REDIS_PASSWORD` 全部删除（redis 是 `redis-server --appendonly yes`，**没有
> `--requirepass`**，那个 env 只是让清单**看起来**有密码保护）；StatefulSet 探针改
> `-U postgres -d quant_trading`。compose 侧 `POSTGRES_PASSWORD` 改读
> `${DATABASE_PASSWORD:-postgres}`、删两处零读取者的 `DB_PASSWORD`、给 data-service
> 补齐四项。
>
> **新增 `pkg/storage/dsn.go`**：`cmd/analysis` 与 `cmd/data` 共用**唯一**的 DSN 拼装
> 实现（此前两份手写，一份转义一份不转义 —— 「两个机制各自对、接起来就错」的形状）。
> 它拒绝两种此前静默通过的配置：URL 里残留 `${...}`（`ErrDSNPlaceholder`）、凭据为空
> （`ErrEmptyDBUser` / `ErrEmptyDBPassword`）。走 `url.UserPassword` 而不是
> `fmt.Sprintf`，密码里的 `:` / `@` / `/` 不再能把 DSN 拆坏。
>
> **`config/*.yaml`**：`database.url` / `database.password` / `tushare.token` 的占位符
> 改成空串 + 启动期校验。**有意保留的偏差**：`tushare.token` 只改空串、**不**做启动期
> Fatal —— `buildTushareClient` 的注释明示「sync 端点会失败、read 端点仍可用」是有意的
> 设计，改成 Fatal 会动一个产品决定，属独立议题。顺带修掉了 40101 的静默失败：viper 的
> `allowEmptyEnv=false` 把空 env 当成「未设置」→ 回落到字面量 `${TUSHARE_TOKEN}` 当
> token 用 —— 「未配置」伪装成「配置了一个怪值」。
>
> **护栏**：`tools/check_deploy_consistency.py` 新增检查 5~8 —— 5 compose ↔ k8s 的
> `DATABASE_*` / `REDIS_URL` 值必须相同、库名 / 用户名不得与 `config/*.yaml` 漂移、密码
> 只能有一个变量；6 注入的每个 env 名都必须有读取点（**从 Go 源码的
> `Get*` / `UnmarshalKey` / `Sub` / `BindEnv` / `ConfigKey*` 常量推导，不是手写清单**），
> `configMapKeyRef` 不得悬空；7 `config/*.yaml` 不得出现 `${...}`（只看 `#` 之前的部分）；
> 8 死键负向钉（`POSTGRES_HOST/PORT`、`REDIS_HOST/PORT`、`REDIS_PASSWORD`、
> `DB_PASSWORD` 不许回来；`POSTGRES_DB/USER/PASSWORD` 只许出现在 postgres 容器自己的
> env 里）。
>
> **破坏验证**：Python 侧 **13 个用例（P1~P11）全达标** —— 键名回退 / 值回退 / 库名·
> 用户名漂移 / 密码变量分裂 / 注入名回退 / `configMapKeyRef` 悬空 / 占位符回来 / 新增
> 死键 / `REDIS_PASSWORD` 回来 / **把派生 env 名集合置空（破坏检查本身，证明这条检查真
> 的有数据可查）** / 反向「注释里的 `${...}` 不算占位符 → 护栏保持绿」。Go 侧 **4 个用例
> （G1~G4）全达标**，每个都**只红该红的**（5/6 保持绿）；G4 特意连 import 一起收干净，
> 避免「编译失败冒充护栏生效」。**三次误判已记进 PITFALLS**：破坏没红时先怀疑破坏本身
> —— 我把检查 5c 的触发点写成了 configmap（实际是 `config/*.yaml`），另外漏实现了检查
> 5d。提交 `5e6da67`。
>
> **AUD-40 落地说明（2026-09-22）**：`v.Sub("backtest")` 在缺段时返回 **nil
> `*viper.Viper`**，`.Unmarshal` 直接 panic（栈底 `viper.(*Viper).AllKeys` 解引用 0x0，
> viper v1.18.2 `viper.go:2069 ← :1118`）—— **不是返回错误**，所以 `if err != nil` 永远
> 不会救场。改成先接住 `Sub` 的返回值，nil 就跳过整段 `backtest.*` 解引用，让默认值逻辑
> 照常生效（缺段是**合法**配置）。全仓只有这一处 `viper.Sub`（其余 30 个 `.Sub(` 都是
> `time.Time.Sub`）。护栏 `TestNewEngine_WithoutABacktestBlockDoesNotPanic` 用
> `viper.New()` 建引擎，断言不 panic **且**默认值落上（「没崩」与「按默认值启动」是两件
> 事）。**破坏验证逐条 `-run` 单跑** —— 这次的失败形态是 panic，会中止整个测试二进制，
> 一次跑全包时排在后面的用例根本不会执行，拿不到区分度证据；期望值写的是 **PANIC 而不是
> FAIL**（断言根本没机会跑，看到 PANIC 才是「抓到了原来那个 bug」的证据）。破坏态：目标
> 用例 PANIC，其余 5 条保持 PASS。提交 `ceff12c`。
>
> **AUD-39 / AUD-40 共同验证（2026-09-22）**：`gofmt -l`（本批改动文件）0 命中；
> `go build ./...` / `go vet ./...` / `GOOS=windows go vet ./...` 全绿；
> `go test ./... -count=1` **73 个包 ok、0 失败**；`check_doc_links.py` 52 文件无坏链；
> `check_deploy_consistency.py` **七项全绿**。两个提交：`5e6da67`（AUD-39）/
> `ceff12c`（AUD-40）。**新登记 AUD-41（SPEC 的 Configuration 草图）/ AUD-42
> （testutil 第三处 DSN 拼装），本批未修。**

> **AUD-42 落地说明（2026-09-22）**：`TestDBConfig.DSN()` 从手写 `fmt.Sprintf`
> 改为委托 `storage.BuildDSN`，签名 `string` → `(string, error)`。三个调用点各自
> 接住错误（`NewTestDB` / `SkipIfNoDB` 走 `t.Skipf` 保持「没有库就跳过」的既有语义，
> `AssertDBAvailable` 返回 false）。
>
> **前提核实推翻了登记的一处**：登记写 testutil 是「测试基础设施」，暗示它有人用 ——
> **实测全仓零导入者**（`go list -f` 含 `.Imports` / `.TestImports` / `.XTestImports`
> 权威核实；Go / 文档 / yaml / sh 全扫只有它自己那条记录）。它是**从未接上的**集成
> 测试底座（归档审查报告 `review-report-20260921.md` 曾把它列为「有 DB 环境下可选
> 集成验证」的**设想**）。**另立 AUD-43** 记这件事，本项只做 DSN 收敛（若曦裁决）。
>
> **护栏两层**：
> ① `pkg/testutil/testdb_test.go`（新增，5 条用例）—— 绝对值断言 + 推导式断言（必须
> 等于 `storage.BuildDSN` 的结果）、密码含 `:` `@` `/` 时原样取回（**配对照组**：
> 朴素 `fmt.Sprintf` 的结果与 BuildDSN 不同、且 userinfo 边界确实被 `@` 抢走）、
> 空密码报 `ErrEmptyDBPassword`（含前置断言）、`TEST_DB_*` 命名空间 + 空串算「未设置」。
> ② `pkg/storage/dsn_single_impl_test.go`（新增，全仓结构护栏）—— 任何**字符串字面量**
> 里出现带 printf 动词的 postgres URL，必须在 `dsnAssemblyAllowlist` 里具名 + 写理由。
> **用 `go/ast` 解析字面量而不是文本 grep**：文本 grep 分不清「调用」与「注释里的
> 提到」，会护栏自己的文档误报（AUD-29 的教训）。
>
> **破坏验证 9/9 达标**（`.workbuddy-ai/tmp/aud42_sabotage.py`）：结构侧 S1 非允许清单
> 文件塞字面量→红且指名 / S2 同一字面量塞进允许清单文件→绿（允许清单生效）/
> S3 写成注释→绿（AST 不受注释干扰）/ S4 从允许清单删掉一条→红（允许清单承重）/
> S5 扫描范围改成空目录→绿（说明「绿」本身不是证据）；行为侧 G1–G4 **各自只红一条**、
> 其余 4 条保持绿。**两次 S5 打空都记下来了，都是破坏方式错不是护栏失效**：用 `^$`
> 当「永不匹配」是错的 —— 它匹配**空字符串**，而仓里几乎每个 .go 都有 `""` 字面量
> → 破坏变成「全红」；换 `zzz_never_matches_zzz` 也不行 —— 替换进去的那个字面量
> **就是**新模式 → 自指命中，只红它自己。**正解是改扫描范围而不是改模式。**
> 提交 `f1b9798`。

> **AUD-41 落地说明（2026-09-22）**：`docs/SPEC.md` 的 `## Configuration` 段
> **整段重写**（原 1487–1601 共 117 行草稿 → 新 154 行如实描述）。
>
> **登记只说对了一半**：登记写「描述了一个不存在的 `config/global.yaml`」，实测
> **两份文件都不存在** —— `config/global.yaml` **和** `config/strategies/value_momentum.yaml`；
> 后者实际是 Go 实现 `pkg/strategy/examples/value_momentum.go`（**Strategy Config 那
> 半段同样要重写**，登记没提）。另外草稿里的 `app.*` / `database.name` /
> `database.max_connections` / `redis.host|port|password|db` / `logging.output` /
> `services.*.port` **逐条 grep 确认全无读取点**。
>
> 新版内容：① 三份真实配置文件 + 各自**定位方式**（analysis 走 `CONFIG_PATH` env，
> data / strategy 走 viper `SetConfigName` + 三个搜索路径 —— 这个不统一是**有意保留
> 的历史差异**，写进去免得下一个人当成 bug 去「统一」）；② 键名 → env 名规则
> （`AutomaticEnv` + `.` → `_`，本仓唯一规则）；③ 主要配置段导航表（段 / 键 / 读到哪）；
> ④ `${...}` 禁令 + 凭据字段三条语义（`password` 空 Fatal / `tushare.token` 空**不**
> Fatal 且写明理由 / `database.url` 整串逃生口）；⑤ 策略 YAML 真实 schema
> （`pkg/ai/yaml.Config`）+ `LoadStrategy` 的**加载条件**（三选一，否则报错）+
> `expression.risk` 与顶层 `risk` 的**层次差异**（同名不同层）。
>
> **护栏 = `tools/check_doc_links.py` 新增的第二项检查**：文档里引用的
> `config/` · `deploy/` 路径必须存在。**这类引用不是 Markdown 链接**，原来的检查
> 根本看不见它 —— 这正是本段能烂掉而无人发现的原因（R5 的实现缺口）。
>
> ⚠️ **第一版护栏抓不住它要防的那个 bug —— 这一条是本项最重要的发现。**
> 第一版只认**反引号里**的路径，而 AUD-41 的原始形态是
> `### Global Config (config/global.yaml)` —— 一个**没有反引号**的标题。
> 实测：把第一版对 `381e60a:docs/SPEC.md` 跑一遍，**零命中**。
> 「护栏写好了、正常态是绿的」完全掩盖了「它对目标形态是瞎的」。
> 修正后的检查：① **匹配裸路径**（反引号可有可无，用 lookaround 排除更长路径的
> 片段）；② 路径按**仓库根或文档自身目录**双基址解析
> （`docs/hermes/system-design.md` 里写的那个相对路径指的是它自己的
> `docs/hermes/config/hermes.yaml` —— 只按仓库根解析实测误报 2 条）；
> ③ 否定词豁免窗口 = **当前行 + 上一行**（散文会换行，否定词常落在路径的上一行；
> 首版用「引用之前」实测漏放 3 条历史行 —— `删 \`cmd/ai/\` + \`config/ai-service.yaml\``
> 这类句子里否定词在引用**之后**）。三条边界都写进了脚本注释。
>
> **破坏验证 10/10 达标**（`.workbuddy-ai/tmp/aud41_sabotage.py`）：
> D1 注入**无反引号**的不存在路径→红且指名（不再依赖反引号）/
> D2 注入存在的路径→绿 / D3 不存在的路径 + 同行否定词→绿 /
> D3b 不存在的路径 + **上一行**否定词→绿（换行豁免）/
> D4 坏 Markdown 链接→红（证明重构没把检查 1 改坏）/
> D5 把**判定逻辑**置成恒真 + D1 的注入→绿（说明「绿」本身不是证据）/
> D6 清空否定词表 + D3 的注入→红（豁免承重）/
> **D7 历史版本 `381e60a:docs/SPEC.md`→红且指名那个**不存在**的 `config/global.yaml`
> （最强的一条：护栏**本来就能**抓到那个 bug —— 规矩 15「对修复前的代码是红的」
> 比「对修复后是绿的」有说服力）** /
> D8a 在 `docs/hermes/` 的文档里写相对路径 `config/hermes.yaml`（仓库根下**没有**它，
> 但文档目录下存在）→绿（双基址生效）/
> D8b 同一路径写在 `SPEC.md`→红（D8a 的绿不是「无脑绿」）。
>
> **第三次踩「置空方式自指」**：D5 最初把正则换成 `zzz_never_matches_zzz`，
> 结果护栏去扫 `.md` 时命中了 **`docs/TASKS.md` 里我写的那句散文**（描述 AUD-42
> 同类踩坑时正好写了这个字符串）→ 破坏变成「红」而不是「绿」。
> **「把检查置空」的替换文本本身可能出现在被扫描的语料里** —— 这是本会话第三次
> 同一类踩空（前两次见 AUD-42 落地说明）。正解是改**判定逻辑**，不引入新字符串。
> 提交 `c666076`（第一版）+ 后续提交（覆盖缺口修正）。

> **AUD-41 / AUD-42 共同验证（2026-09-22）**：`gofmt -l`（本批改动文件）0 命中；
> `go build ./...` / `go vet ./...` / `GOOS=windows go vet ./...` 全绿；
> `go test ./... -count=1` **74 个包 ok、0 失败**（比上批 +1 —— `pkg/testutil`
> 现在有测试了）；`check_doc_links.py` 52 文件、**两项检查**全绿；
> `check_deploy_consistency.py` 七项全绿。两个提交：`f1b9798`（AUD-42）/
> `c666076`（AUD-41）。**新登记 AUD-43（`pkg/testutil` 全仓零导入者，去留待裁）/
> AUD-44（SPEC · TEST · ADR 三份常青文档不符合 R1 frontmatter 约定），本批未修。**

---


> **AUD-43 / AUD-45 落地说明（2026-09-22）**：**前提订正推翻了登记的判据。**
>
> 登记 AUD-43 时只发现 `pkg/testutil` 一个零导入者包。开工前用
> `go list -f '{{.ImportPath}}|{{join .Imports " "}}|{{join .TestImports " "}}|{{join .XTestImports " "}}' ./...`
> 权威核实，实测 **13 个**：`pkg/api` / `pkg/decimal` / `pkg/metrics` /
> `pkg/ai/factor` / `pkg/backtest/auction` / `pkg/backtest/marketimpact` /
> `pkg/alert/systemalert` / `internal/sandbox/wasm` / `pkg/live/margin` /
> `pkg/strategy/options` / `pkg/data/source/hkex` / `pkg/live/broker/xtp` /
> `pkg/testutil`。逐包 `grep` 双重确认（外部引用文件数全为 0）。
>
> **「零导入者」在本仓不是删除判据** —— 台账里已有明确立场：**P2-5「删的是服务
> 不是能力」**（`pkg/ai/agents` 不能删，因为 `pkg/ai/pipeline` 在用）、
> **P2-9wire「五片全写完时整包零调用（'5/6 片的零件 + 0 接线'）」**。那 12 个都是
> **有实现、有测试的生产能力包**（Black-Scholes / 二叉树、融资融券、XTP 券商、
> WASM 沙箱、港股源、集合竞价、市场冲击、系统告警、因子、定点小数、Prometheus
> 指标、API 版本化），只是尚未被编排层接线。所以「零导入者 ⇒ 删除」与仓库既有
> 立场冲突 → 按纪律**订正登记，不绕过**：若曦 2026-09-22 裁决 **AUD-43 保留
> `pkg/testutil` 并归并入 AUD-45**。
>
> **AUD-45 = 统一盘点 + 结构性护栏**（`5e16fbe`）：新增
> `internal/repoguard/package_wiring_test.go` —— 断言 **`pkg/**` 与 `internal/**`
> 下每个包都至少有一个导入者**，例外必须进 `unwiredPackages` 白名单并写明理由。
> 三条设计要点：① **范围从源码推导**（`go/ast` 扫全仓 `.go` 的 import），不是手写
> 清单（PITFALLS §41）；② **白名单自带 stale 检测** —— 某个包一旦被接上，它的
> 白名单条目会**报错要求删除**（白名单只该列「当前无人用」的包，否则它在替死代码
> 打掩护）；③ **边界写进脚本**：只查 `pkg/` `internal/`（`cmd/` 是入口，天然无
> 导入者）；只有 `_test.go` 的目录跳过（不可被导入，零导入者无意义）；import 收集
> **不看 build tag**（平台门控的导入也算数 → 只会漏报、不会误报）；**不检测传递性
> 死代码**。
>
> **破坏验证 5/5 达标**（`.workbuddy-ai/tmp/aud45_sabotage.py`）：S1 删白名单条目
> （`pkg/decimal`）→ 红且**只指名它**；S2 新建无人导入的包 → 红指名它；S3 导入收集
> 失效 → 红（报出全部候选包，证明收集是承重的）；S4 给白名单里的包接上导入 → 红并报
> **stale**；**S5（保持绿对照）候选枚举失效 → 绿**，证明前面的红来自候选枚举而不是
> 「恒红」。
>
> **AUD-44 落地说明（2026-09-22）**（`85cf38c`）：三份入口文档补 R1 frontmatter。
> `docs/SPEC.md` / `docs/ADR.md` / `docs/TEST.md` 此前用的是**自创的 blockquote
> 元数据**（SPEC/TEST 是 `> **Status**:` + `> **Last Updated:**`，ADR 连 `status`
> 都没有）—— R1 的 `status` / `last-verified` / `verified-by` 一个都没有。改法：
> 加 YAML frontmatter，并把 blockquote 里被 frontmatter 接管的那两行删掉
> （**避免两处真相来源**）。
>
> ⚠️ **`docs/TEST.md` 不只是格式问题，它确实腐烂了**：`pkg/tracker` **已不存在**
> （迁到 `pkg/backtest/tracker`）、`docs/phase-gate-reviews.md` **已归档**（现在在
> `docs/archive/research-2026-Q2/`）、§4 的 `go run ./cmd/analysis/main.go --strategy ...`
> **不是真实接口**（analysis-service 是 HTTP 服务，回测走 `POST /api/backtest`）。
> 三处已修，但 **§5 / §6 / §7 的判据没有逐条复核** —— 所以 `last-verified` 的
> `verified-by` **如实写明只验了路径引用**，剩下的事登记为 **AUD-47**。
> **不盖一个自己没做过的章** —— 这正是 R1 存在的意义：`last-verified` 必须是真的。
>
> **护栏 = `tools/check_doc_links.py` 新增的第三项检查**：入口层文档必须带 R1 三字段。
> 范围**从 `docs/README.md` 的链接推导**（README 是文档的唯一入口，被它链接的就是
> 「新来的人会读的那几份」），不是手写清单 —— 加一份新入口文档时护栏会自动要求它。
> 只认**文件开头**的 `---` frontmatter 块（散在正文里的 `status:` 不算数，那正是
> SPEC/TEST 过去的形状）。
>
> **破坏验证 5/5 达标**（`.workbuddy-ai/tmp/aud44_sabotage.py`）：F1 删 SPEC 整个
> frontmatter → 红指名它；F2 只删 ADR 的 `last-verified` → 红并说明缺哪个字段；
> F3 把 frontmatter **挪出文件开头** → 红（证明「只认开头」）；F4 新增一份无
> frontmatter 的文档并链进 README → 红指名它（证明范围确实是推导出来的）；
> **F5（保持绿对照）检查恒返回空 → 绿**，证明前面的红来自检查本身。
>
> **顺带登记**：**AUD-46**（13 个零导入者包里 `pkg/metrics` 与 `pkg/observability`
> **重复实现**、`pkg/decimal` **全仓零引用** —— 这两个是「疑似真死代码」不是
> 「待接线」，需裁决删除还是采用；已临时进白名单，**白名单不是结论**）/
> **AUD-47**（`docs/TEST.md` §5–§7 的内容复核）。
>
> **AUD-43 / AUD-44 / AUD-45 共同验证（2026-09-22）**：`gofmt -l`（本批改动文件）
> 0 命中；`go build ./...` / `go vet ./...` / `GOOS=windows go vet ./...` 全绿；
> `go test ./... -count=1` **75 个包 ok、0 失败**（比上批 +1 —— 新增
> `internal/repoguard`）；`check_doc_links.py` 52 文件**三项检查**全绿；
> `check_deploy_consistency.py` **七项**全绿。提交：`5e16fbe`（AUD-45）、
> `85cf38c`（AUD-44）。


> **AUD-46 / AUD-47 落地说明（2026-09-22）**：**AUD-45 的盘点把 13 个零导入者包
> 放进了同一个桶（「零导入者」），本批证明这个桶里装的不是同一种东西。**
>
> **AUD-46 —— `pkg/metrics` 是「竞争实现」，不是「待接线」**（`cba6bfe`）：
> 取证把登记里的模糊判断换成了精确事实 —— **ADR-017 §1 定义的那四个核心指标**
> （`backtest_duration_seconds` / `http_client_requests_total` / `llm_tokens_total` /
> `cache_hit_ratio`）**正是 `pkg/observability` 实现的那四个**
> （`deps.Metrics *observability.Metrics`，`main.go:319` →
> `observability.Handler(deps.Metrics)`）。`pkg/metrics` 是另一套 **7 个**指标，
> 与它**只重叠一个名字**（`backtest_duration_seconds`）—— 所以「保留并接线」
> **技术上不可行**：两者同时注册会**重复注册该指标并 panic**。
> → 若曦 2026-09-22 裁决**删除**（要加指标请加进 `observability`，一个注册表）。
> **`pkg/decimal` 裁决保留**，白名单理由从「需裁决」改为「**待采用**：portfolio /
> 回测仍用 float64，迁移未排期」—— 它是能力包，与另外 11 个同型。
>
> **退役 = 删代码 + 加一条「它不许回来」的断言**（AUD-33 的教训，AUD-45 已把它
> 写成护栏）：`internal/repoguard` 新增 **`retiredPackages`** —— 对每个退役包断言
> **目录不存在**（用 `os.Stat`，所以**连只放 `_test.go` 的纯测试目录也算复活**）。
> 破坏验证 **4/4**（`.workbuddy-ai/tmp/aud46_sabotage.py`）：T1 重建 `pkg/metrics`
> （有非测试文件）→ 红指名它；T2 **只重建测试文件** → 仍红（证明判的是目录存在、
> 不是候选集）；T3 什么都不改 → **绿（保持绿对照）**；T4 删白名单条目
> （`pkg/decimal`）→ 红（证明原有的「零导入者」检查没被这次编辑改坏）。
>
> **AUD-47 —— `docs/TEST.md` §5–§7 内容复核**（`ca5e547`）：AUD-44 只验了路径引用，
> 本批把 §5–§7 逐条对齐实现 —— **四处都错了**：
> ① §5 沙箱限制写「`GenerateSignals()` ≤ 5s / 内存 ≤ 100MB」，实测是
> **30s CPU + 1 GiB**（ODR-020，见 `cmd/analysis/main.go` 的 `sandboxRunnerAdapter`）；
> ② §6「Coverage Targets」**在 CI 里没有门禁** —— go job 只跑 `go build` / `go vet` /
> `GOOS=windows go vet` / `go test -count=1 -race`，**不读 `coverage.out`**；
> `make test` 只生成报告。已加显式警告「**这是目标，不是门禁**」；
> ③ §7.1 `format.test.ts` 写 8 tests，实测 **28**；
> ④ §7.2 e2e 写 3 个套件共 22 例，实测 **17 个 spec / 160 例**；顺带修
> `--project=chrome` → **`chromium`**（配置里唯一注册的 project 名）。
> 改完 `last-verified` 才如实升到 2026-09-22（`verified-by` 列出验了哪四处）。
> **教训**：AUD-44 说「§5–§7 未核」时，那是**老实话**；本批把老实话变成了核对 ——
> 一份「只验了路径」的文档，剩下的部分几乎必然也是错的（四处无一例外）。
>
> **AUD-46 / AUD-47 共同验证（2026-09-22）**：`gofmt -l`（本批改动文件）0 命中；
> `go build ./...` / `go vet ./...` / `GOOS=windows go vet ./...` 全绿；
> `go test ./... -count=1` **74 个包 ok、0 失败**（比上批 **−1** —— `pkg/metrics`
> 已删）；`check_doc_links.py` 52 文件**三项检查**全绿；
> `check_deploy_consistency.py` **七项**全绿。
> **AUD 线到此全部关闭**（AUD-01 ~ AUD-47）。

> **AUD-48 落地说明（2026-09-22 深夜）**：**不是审查登记项，是做本地完整部署时
> 撞出来的。** 若曦问「`TUSHARE_TOKEN` 已经在 Windows 环境变量里，是不是没做最新
> 的本地 deployment」—— 逐跳验下来 token 确实设了（len=56，注册表里读得到），
> 但**宿主 shell 会话没继承** + `.env` 里是**空串兜底** → compose 解析成 `""`
> → 容器里是空。于是执行完整部署，构建阶段直接炸：
>
> ```
> [analysis-service stage-1 8/8] COPY cmd/analysis/static /app/cmd/analysis/static
> ERROR: failed to calculate checksum: "/cmd/analysis/static": not found
> ```
>
> **这才是「6 个服务只起得来 3 个」的真正原因 —— 不是忘了部署，是 analysis /
> strategy 两个镜像从 AUD-33 那天起就构建不出来。** 容器 `ps -a` 里连 Exited
> 的都没有，因为它们**从未被创建过**。
>
> **三处断裂（同一个病根）**：
>
> | 文件 | 行 | 引用了什么 | 谁用它 |
> |---|---|---|---|
> | `Dockerfile.service` | 44 | `cmd/analysis/static`（AUD-33 已删） | compose 的三个服务 |
> | `cmd/analysis/Dockerfile` | 35 | 同上 | `Makefile` 的镜像目标 |
> | `cmd/strategy/Dockerfile` | 36 | `cmd/strategy/generated_strategies/`（**全仓零引用**的孤儿） | `Makefile` 的镜像目标 |
>
> 前两处是护栏加上之后自己抓出来的第三处 —— 又一次印证「单一样本看起来像特例，
> 实测常是一类里的一员」（PITFALLS §50）。
>
> **护栏（`check_deploy_consistency.py` 检查 9）**：解析全仓 `Dockerfile*` 与
> `cmd/*/Dockerfile`，断言**不带 `--from` 的 `COPY`** 的源路径存在。
> 边界写进脚本：① 带 `--from` 的源是**另一个构建阶段内部**的路径，不在 build
> context 里，静态查不了 → 跳过；② build context 假定为仓库根（compose 的
> `context: .` 与 Makefile 的 `docker build -f ... .` 都是根）；③ 支持通配符与
> 多源；④ `COPY . .` 跳过（`pathlib.glob` 不接受 `.`，会抛 ValueError —— 这个
> 坑是跑第一版时撞到的）。
>
> **破坏验证**：把 `COPY cmd/analysis/static` 加回 `Dockerfile.service` →
> **红且指名 `Dockerfile.service:44`**，退出码 1，其余 7 项不受影响；反向替换恢复
> 后回到全绿。
>
> **元教训（与 ODR-021 那条注释同型，已第三次）**：**删目录时，引用它的 Dockerfile
> `COPY` 不会有任何编译期告警** —— `go build` / `go vet` / `gofmt` 都不读 Dockerfile，
> 而 CI **不构建镜像** → 断裂长期静默，症状不是「构建失败」而是「那个服务从来没
> 起来过，而且看起来没人动过它」。**删目录后要 grep `Dockerfile`**（现在有护栏了）。

> **P2-13 落地说明（2026-09-23）**：**验证器链接真库取证**
>（`pkg/validation/live_backtest_integration_test.go`，新建 1 例）。
>
> **登记的前置被放大了。** 原登记写「跑数据同步补齐行情后，用同一范式接真库」，
> 读起来像要等 5907 只票全部同步完。实测：**有 65 只票带 3 年完整历史（725 根 K 线）
> 时就能取证了** —— 那是同步跑到第 100 只左右的事。**「等全量」不是技术前置，
> 是心理前置。**
>
> **接的是真库，不是测试库。** 用 `storage.NewPostgresStore` +
> `marketdata.NewPostgresProvider` 连 `quant_trading`。**没有用
> `pkg/testutil.NewTestDB`** —— 它的默认库是 `quant_trading_test`，连过去只会
> 得到一个空库，然后测试以「库里没数据」skip，**永远绿**。这是「断言落在不是生产
> 消费的那一层」的另一种形态：**连的库不对，测的就不是同一份数据**。
>
> **池子按代码顺序取，不按历史完整度排。** 按完整度排是在用未来信息挑样本
>（排前面的必然是活到今天的票），等于把这一维要查的幸存者偏差自己造出来。
> 按代码顺序取自然带进退市票 —— 实测 40 只池子里有 **3 只在区间内摘牌**
>（`000004.SZ` 国华退 2026-07-14、`000005.SZ` ST星源(退) 2024-04-26 等），
> 测试**断言这个数必须 > 0**，否则直接失败：池子被幸存者选过，这一维的结论就不成立。
>
> **首次实测结果：验证器链在真数据上否掉了这个策略。**
>
> | 维度 | 概率 | 说明 |
> |---|---|---|
> | statistical | 0.1434 | 3 次尝试校正后 p=0.857，Sharpe=-0.46 不显著 |
> | economic | 0.0021 | 毛 -31.48%、成本 7.56%、净 **-39.04%**（年换手 10.5 倍 × 602 交易日） |
> | robustness | 0.0000 | 3 个邻近参数 0% 站得住 —— 尖峰不是高原 |
> | bias | 0.8746 | 池子含 3 只退市票（好迹象）+ 前复权自带前视成分（如实提醒） |
> | redundancy | 0.7044 | 与同策略另一组参数的相关性 |
> | causal | — | **如实记未评估**（要一次 LLM 调用，不拿剩下五维凑数） |
>
> **综合概率 0.0000、最弱维 robustness、3 条 blocking。** 这才是这一维该有的样子：
> **它愿意说不。** 一条在真数据上只会给高分的验证器，校准不了任何东西。
>
> **故意留空的两个字段**：`PITVerified` / `DataLagKnown` 都没填。这次跑的 momentum
> 只吃价量，而「引擎逐日喂 K 线、不预读未来 bar」这件事本测试**没有独立验证过**
> —— 写 `PITVerified=true` 就是**盖一个没做过的章**（PITFALLS §53）。留空 →
> 前视子维度记入未评估，如实暴露，并附带一条 note。
>
> **破坏验证（S1）**：把 `Existing: existing` 改成 `Existing: nil`（**保留变量被使用，
> 避免变成 build failure** —— 编译不过不算护栏生效）→ `go vet` 通过、测试**只红该红的**：
> 指名 `[redundancy]`，其余四维照旧评估。反向替换恢复后 `diff` 与备份**字节相同**、
> `grep -c SABOTAGE` = 0、整包 `ok github.com/ruoxizhnya/quant-trading/pkg/validation 75.5s`。
>
> **顺带订正的取证细节**（写之前跑过命令）：`trading_calendar` 实际 **1097 行 /
> 区间 2023-09-23 ~ 2026-09-23 / 其中 726 个交易日**。此前台账与工作记忆里的
> 「2194」是错的 —— 该表主键是 `trade_date` 单列，**不可能有重复行**。

> **AUD-49 / AUD-50 登记说明（2026-09-23）**：两项都是**打通数据同步时撞出来的**，
> 不是审查登记项。同族 —— **「两个机制各自对、接起来就错」**（本仓已第十例）。
>
> **AUD-49 —— `POST /api/sync/jobs/:id/cancel` 对 `running` 任务无效。**
>
> 症状：接口返回 `{"message":"job cancelled","job_id":"..."}` 且 HTTP 200，
> 但 `processed_items` **继续往上走**。现场只能 `UPDATE sync_jobs SET status='cancelled'`
> 手工收拾。
>
> 机制（两层，都读过源码）：
> ① `JobService.CancelJob`（`pkg/sync/job.go:154-177`）只把**库里的行**改成
> `cancelled`；而 worker 手里那个 `*Job` 是 `Queue.Dequeue` 时取的内存副本，
> 它完全不知道这件事。
> ② 执行器每隔一秒调 `jobProgressReporter.ReportProgress` → `queue.UpdateJob(ctx, r.job)`
> → `PostgresStore.UpdateSyncJob`，而那条 SQL 是
> **`UPDATE sync_jobs SET status = $2, ... WHERE id = $1`** ——
> **`WHERE` 只有 id，却把内存里的 `status`（还是 `running`）写回去**。
> 于是**下一次进度上报必然把 `cancelled` 复活成 `running`**。
> ③ 附带：`workerLoop` 用的是 `jobCtx := context.Background()`（`worker.go:132`），
> **根本不存在 per-job 的取消句柄** —— 所以即使 ① 写对了，也停不下执行器。
>
> **AUD-50 —— 重启后 `running` 的 sync job 永久搁浅。**
>
> 症状：`docker restart` 之后任务永远停在 `done=20/5907`，日志里只剩 `/health`。
>
> 机制：`Queue.Dequeue` 每次都查库，但**只查 `pending`**
>（`q.store.ListSyncJobs(ctx, JobStatusPending, 1)`）。进程重启时正在跑的那一行
> 留在 `running`，**没有任何代码把它改回 `pending`**，于是三个 worker 全在
> `WaitForJob` 上睡到天荒地老。
>
> ⚠️ **先例存在、也接上了线，但只接了半条**：`pkg/backtest/job` 有
> `CleanupStaleRunning`（P0-8），且**有生产调用方** —— `cmd/analysis/setup.go:740`，
> 在 `gracefulShutdown` 里。但它的**文档注释自己写着**「also useful as a recovery
> tool after a hard process crash (kill -9, OOM, etc.) — **call it on startup** to
> repair stale rows from the previous run」，而**没有任何启动期调用点**。
> 也就是说：**文档建议的那条路，从来没人走过。** `pkg/sync` 则连函数都没有。
>
> 两项的修法方向（落地前先按 PITFALLS 的规矩核一遍前提）：
> `WorkerPool` 持有 `jobID → cancelFunc` 注册表，`CancelJob` 走它去真取消；
> 进度上报改成**条件更新**（`WHERE id = $1 AND status = 'running'`），
> 让「已终态的行」不可能被进度上报复活；`pkg/sync` 补
> `CleanupStaleRunning` 并**在 `cmd/data` 启动期调用**（同时给 `cmd/analysis`
> 补上启动期那一半 —— 它现在只做了关闭期那一半）。

> **AUD-50 落地说明（2026-09-23）**：**重启后 `running` 的 job 必须被回收。**
>
> **修法**：`pkg/sync` 新增 `Queue.CleanupStaleRunning`，把 `running` 的行标成
> `failed`（**不是 `cancelled`** —— 没人主动停它，是被中断的；这个区别对后来读
> 台账的人有意义。标 `failed` 也让它可以 `RetryJob`，人工能续跑）。进度字段
>（`processed_items` / `total_items`）保留 —— 那是「中断时同步到哪」的唯一记录。
>
> **调用点放在 `WorkerPool.Start()` 里，不放在调用方**：worker pool 只捞
> `pending`，所以回收必须**在任何 worker 起来之前**做完；而它只依据数据库判断，
> 看不见内存里在飞的任务。把调用点放在 worker 起来的地方，「有人忘了调」就在
> **结构上不可能**，而不是靠注释提醒。
>
> **`cmd/analysis` 补上另一半**：backtest 的 `CleanupStaleRunning` 一直只接了
> 关闭期（`gracefulShutdown`）。它的文档注释写着「call it on startup to repair
> stale rows from the previous run」，而**启动期调用点从来不存在**。现在
> `buildDataServices` 里补上了。这一半**做不到**「结构上不可能忘」——
> backtest 任务跑在自己的 goroutine 上，没有中心化的 dequeue 可以挂钩，所以
> 「只在启动期调用」这个前提只能**写下来**，并由下面的护栏钉住调用点。
>
> **新增 `internal/repoguard` 的函数级接线护栏**
>（`TestRecoveryFunctionsAreCalledFromTheRightPlace`）：判据是
> **「从 `file:function` 里被调用」** —— 不是「函数存在」（存在正是缺陷的前提），
> 也不是「有某个调用点」（backtest 那个一直有，只是在错的路径上）。
>
> ⚠️ **这条护栏的第一版是瞎的，是破坏验证抓出来的。**
> 第一版钉的是**文件**（「`cmd/analysis/setup.go` 里有调用」）。把新加的启动期
> 调用删掉 → **护栏仍然绿**，因为同文件的 `gracefulShutdown` 还在调同一个方法。
> 更要紧的是：**第一版在修复前的代码上本来就是绿的**（setup.go 一直有调用，
> 只是从不在启动期）。**一条对「它要防的那个 bug」是绿的护栏不算护栏。**
> 改成钉 `file:function` 后重跑同一个破坏 → 红，且诊断输出正是证据：
> `[cmd/analysis/setup.go:gracefulShutdown pkg/sync/worker.go:Start]` ——
> 关闭期那一半在、启动期那一半缺。
>
> **破坏验证（S2 / S3）**：
>
> | 破坏 | 期望 | 实测 |
> |---|---|---|
> | S2：摘掉 `WorkerPool.Start` 里的回收调用 | 只有**接线测试**红 | ✅ `go vet` 通过（不是 build failure）；3 条队列级单测**保持绿**（证明它们抓不住接线断裂）；`TestWorkerPool_StartRecoversInterruptedJob` 红并指名原因；**不依赖该功能的** `StartDoesNotEatPendingJobs` 保持绿 |
> | S3：摘掉 `buildDataServices` 里的启动期调用 | 函数级护栏红 | ⚠️ 第一版护栏**没红**（见上，已修正）→ 修正为 `file:function` 后红，并打印现有调用点 |
>
> **顺带**：`TestProductionCallSiteWalkerSeesTheTree` 是「护栏的护栏」——
> 断言遍历真的走到了仓库里（否则「找不到调用点」是假的），且调用点标签确实是
> `file:function` 形式（退化成只有文件名就会重新变瞎）。
>
> **护栏的已知边界（写进脚本注释）**：按**方法名**匹配、不做类型解析，所以两个
> 不同包的同名恢复方法对遍历器不可区分（今天它们在不同文件里被调用，钉
> `file:function` 足够）。**若哪天它们从同一个函数里被调用，这条护栏就分不出来**
> —— 要么别这么做，要么先教会遍历器解析 receiver。

> **AUD-49 落地说明（2026-09-23）**：**取消必须「行」和「执行器」两半都到位。**
>
> **判据取的是现场症状**，不是「接口返回什么」：`CancelJob` 一直返回成功
> （HTTP 200 + `{"message":"job cancelled"}`），真正的症状是
> **`processed_items` 还在往上走**。所以断言分两层，分别由两条测试守住。
>
> **修法（一）：把「写」变成条件写，并且只留一个入口。**
> `pkg/storage` 的无条件 `UpdateSyncJob`（`WHERE id = $1`）**整个退役**，
> 换成 `UpdateSyncJobIfStatus(ctx, job, from ...JobStatus) (bool, error)`：
> `WHERE id = $1 AND status = ANY($n::text[])`，并返回 `RowsAffected() > 0`。
> **条件必须在 SQL 里**，不能写成 Go 的「读—比—写」—— 那一对语句对每个写者都在竞争。
> 退役做的是「一个决定只有一个入口」：全仓 `.UpdateSyncJob(` 只有 8 处调用点
> （`pkg/sync` 6 处 + `JobService` 2 处），全部转到新方法后旧名**不再存在**，
> 第二个入口在编译期就没了。
> `from` 为空**报错**而不是匹配全部：`ANY('{}')` 匹配不到任何行，
> 一个忘了传参的调用会静默退化成空操作。
>
> 所有 worker 侧写路径都改成条件写，来源状态一律 `running`：进度上报
> （`UpdateRunningJob`，**替代并删除**了原来的 `Queue.UpdateJob`）、
> `CompleteJob`、`FailJob`、`RetryLater`、`Dequeue` 的领取、`CleanupStaleRunning`。
> 其中 **`RetryLater` 是最危险的一条**：它把行改成 `pending`，
> 于是 worker 会**把用户已经取消的任务再跑一遍** —— 同一个缺陷换了顶帽子。
>
> **修法（二）：给执行器一个真句柄。**
> `workerLoop` 原来是 `jobCtx := context.Background()`，**per-job 取消句柄在原理上
> 就不存在** —— 所以即使数据库写对了，执行器也会继续对着 Tushare 跑几小时。
> 现在 `WorkerPool` 持有 `jobID → *runningJob{cancel}` 注册表，`workerLoop` 用
> `context.WithCancel(wp.ctx)` 并在 `processJob` 前后登记/注销（注销按**指针同一性**
> 判断，避免重试后的新句柄被上一轮的清理误删）；
> `JobService.SetRunningCanceller(workerPool.Cancel)` 由
> `cmd/data/sync_handlers.go:NewSyncHandler` 接上，**调用点由 `internal/repoguard` 钉住**。
>
> **顺序：先结算行，再发信号。** 因为 worker 侧写路径全都条件于 `running`，
> 一旦行是 `cancelled`，在飞的 worker 就再也写不回去 —— 下一次进度上报、完成、
> 重试都不行。反过来先发信号会开一个窗口：任务合法地跑完了，而取消随后报一个
> 令人困惑的失败。
>
> **`ctx` 已结束就不再写任何状态。** `processJob` 里这条规则是一句话：
> **上下文已取消的任务不写库** —— 它的行要么已被取消者结算，要么留给
> `CleanupStaleRunning`（AUD-50）回收。没有这条规则，被取消的任务会立刻试图用
> **已经死掉的 ctx** 去写 `failed`（或经重试写 `pending`）：写不进，而且意图本身就是错的。
>
> ⚠️ **行为变更（记下来，别当没发生）**：per-job ctx 现在挂在 `wp.ctx` 下，
> 所以 `Stop()` 会**打断在飞的任务**，而不是像以前那样无限期等它跑完。
> 这不是新增风险：`docker stop` 本来就在 10 秒后 SIGKILL，结果一样；
> 区别只是现在能干净退出，且被中断的行由 AUD-50 回收、可 `RetryJob` 续跑。
>
> **测试与分工（`pkg/sync/cancel_test.go` 13 条 + `pkg/storage/sync_jobs_conditional_test.go` 6 条）**：
> ① 「执行器是否真的停了」由 `TestCancelRunningJob_StopsTheExecutorAndStaysCancelled`
> 与 `TestWorkerPool_RegistersInFlightJobForCancellation` 守；
> ② 「终态不能被写回去」由 `TestQueue_ProgressUpdateCannotResurrectSettledJob`
> 等 5 条窄测守（进度 / 重试 / 失败 / 完成 / 领取竞争）。
> **两层必须分开测**：只测 ① 会漏掉复活路径，只测 ② 会漏掉「执行器根本没停」。
> 存储层的条件**真的在 SQL 里**由真库测试证明 —— 上层单测打的是 mock，
> mock 里可以假装实现一份条件语义，SQL 写错（`ANY` 漏了、参数类型没对上）
> 它们照样全绿，而这正是本仓反复踩的「两个机制各自对、接起来就错」。
>
> **破坏验证（S1 / S2 / S3）**：
>
> | 破坏 | 期望 | 实测 |
> |---|---|---|
> | S1：把进度上报的条件退化成列出全部状态（等价于无条件写） | 只有**进度复活**那条窄测红 | ✅ `vet-exit=0`（不是 build failure）；只有 `TestQueue_ProgressUpdateCannotResurrectSettledJob` 红，信息直指「这就是 AUD-49 的复活路径」；重试 / 失败 / 完成三条**保持绿**（它们走别的方法） |
> | S2：把注册表里的 cancel 换成空函数（`Cancel` 仍返回 true） | 只有**「执行器真停了」**那两条红 | ✅ `vet-exit=0`；`TestCancelRunningJob_...` 红在「执行器没有观察到 ctx 取消」；`TestWorkerPool_RegistersInFlightJob...` 红在「任务结束后必须注销」；② 家族全绿 |
> | S3：摘掉 `cmd/data` 的 `SetRunningCanceller` 接线 | 函数级接线护栏红 | ✅ `go build ./cmd/data/` 仍通过（setter 是可选的，所以是行为破坏不是编译错）；护栏打印 `现有生产调用点（file:function）：[]` |
>
> **S1 与 S2 分别只红各自那一层，是本项最有价值的一条证据**：它说明两半是
> **独立覆盖**的，任何一半单独退化都会被抓住 —— 而不是「两条测试碰巧一起红」。

> **AUD-51 登记说明（2026-09-23）**：**测试的前置条件与断言读的不是同一个东西。**
>
> 症状：完整跑测试时 `TestHasOHLCVData` 红，而**本轮改动与它无关**
> （改的是 `sync_jobs`，它读的是 `ohlcv_daily_qfq`）。取证：直接查库
> `SELECT count(*) FROM ohlcv_daily_qfq WHERE symbol='600000.SH'` → **0 行**，
> 因为 3 年同步还在跑、才走到 `300320.SZ`（当前已同步区间 `000001.SZ`–`300320.SZ`）。
>
> 机制：`pkg/storage/postgres_test.go:170` 的 `TestHasOHLCVData` 断言
> **`600000.SH` 有数据**，而它的前置 `skipIfNoSeedData(t, store, "ohlcv_daily_qfq")`
> 只检查**这张表非空**。「表非空」与「这个 symbol 有数据」是两个不同的事实 ——
> 于是任何**局部同步**（同步途中、或只 `RetryJob` 了一部分 symbol）都会让它红，
> 而这条红**不指向任何代码问题**。这正是 `skipIfNoSeedData` 自己的注释所反对的
> 「失败信号没有意义」，只是它反对的是「库是空的」，没管「库是半满的」。
>
> **同类的潜伏成员 3 个**（按「去数反例密度」的规矩查的，不是只看这一个样本）：
> `TestGetTradingDays` / `TestGetTradingDates` / `TestIsTradingDay`
> 与前置读的不是同一个东西（`postgres_test.go:242/256/395`）。它们至今没发作，
> 只在「两表一空一不空」或「同步到的年份对不上」时才露出来。
>
> ⚠️ **登记时把这三个的病因写混了，落地时逐条对代码订正**（2026-09-25）：
> 表名真正对不上的是 `TestGetTradingDates`（`pkg/storage/calendar.go:91` 读
> `trading_calendar`）与 `TestIsTradingDay`（`calendar.go:115` 同样读 `trading_calendar`）
> —— 前置却查 `ohlcv_daily_qfq`。而 `TestGetTradingDays` 的**表名是对的**
> （`pkg/storage/ohlcv.go:146`，`SELECT DISTINCT trade_date FROM ohlcv_daily_qfq`），
> 它的问题在**区间**：前置只保证「表非空」，断言却要 2024 年 1 月**那一个月**有数据。
> 同一族、「前置不蕴含断言」这个**成因**相同，但**缺口位置**一个在表名、一个在区间
> —— 登记时把三者并成一句话，就抹掉了这个区别。
>
> 修法（**已落地，2026-09-25**）：不是把前置改得「更准」，而是**取消前置** ——
> 四个测试一律改为**自灌自证**（自己写数据、自己断言、自己清理），于是「前置与断言同源」
> 这个要求变成结构上自动成立：断言读的东西就是它自己刚写进去的东西。
> 登记时考虑的两种「打补丁」写法都被否掉了：按具体 symbol 判前置
> （`HasOHLCVData("600000.SH")` 为假就 skip）只是把假警报换成静默跳过，
> 而「从库里查一只票出来再断言」会让测试的读数依赖**哪只票恰好先被同步到**。
> 自灌自证没有这个问题：数据由测试自己造，与同步进度**无关**。

> **AUD-52 登记说明（2026-09-23）**：**e2e 套件的前置条件不再蕴含它的断言。**
>
> 与 AUD-51 **同症状、不同成因**，所以分开登记（同一个桶里的东西要先按成因再分一次）：
> AUD-51 是前置条件**写错了对象**（查 A 表、断言读 B 表），
> AUD-52 是前置条件**不够** —— 套件级的门只保证「服务在」，
> 不保证「服务要的东西你有」。
>
> 症状：完整跑测试时 `e2e/tests` 两条红，**与本轮改动无关**。
>
> 机制：`e2e/tests/integration_test.go` 的 `TestMain` 只有一道门 ——
> 「analysis 可达就整套跑」（`S7-P0-8` 加它，本意是让没起 Docker 的环境
> 退出 0 而不是 FAIL）。但 analysis 现在**是**可达的，于是整套跑，然后：
>
> ① **`executionURL := "http://localhost:8084"`（:248）指向已退役的服务。**
> ODR-021 把 execution 并进 analysis，端点在 `:8085/api/execution/*`
> （`cmd/analysis/handlers_execution.go`），容器里已无 :8084。
> → `TestExecutionService_OrderPersistence` **在当前架构下永远不可能通过**。
> 一条永远不可能绿的测试比没有测试更糟：它会训练人忽略红色。
> ② **`TestStrategyAPI_ListStrategies` 期望 200，实测 401。**
> 打 `:8085/api/strategies`，鉴权上线后套件没有配套的取 token 步骤。
>
> 两者的共同点：**「服务在」和「你能用这个服务」是两件事** ——
> 与「变量『已设置』和『值抵达目的地』是两件事」同型。
>
> 修法方向（未落地）：① 把 :8084 改指 `:8085/api/execution/*`（或直接删掉这条
> 已被 ODR-021 取代的用例）；② 套件启动时取一次 token（或显式
> `E2E_FORCE_SKIP=1` 并在文档里写明它需要什么）；③ 更好的是把「这道门」也做成
> **与断言同源** —— 门应该检查它真正需要的东西（执行端点存在、鉴权可用），
> 而不只是「某个端口有响应」。

## P2-8 取证说明（2026-09-23）

> 取证文件：`pkg/data/tushare_adjustment_integration_test.go`（真库 + 真 Tushare +
> 真引擎）、`pkg/data/tushare_hfq_test.go`（无网络、无库的确定性断言）。

### 一、前置确认（先做的事）

`daily` + `adj_factor` 在容器内实测均有权限（同一个 token，`code:0` 返回真数据）；
`stk_factor_pro` 仍是 **40203**。所以「P2-8 卡在 token」这条**确实已经解除**。

存量 qfq 的基准也核实了：`ohlcv_daily_qfq` 在 **2026-09-23** 的收盘价与 `daily`
返回的**不复权**收盘价逐值相同（`600519.SH` = 1251.24），说明入库时用的分母就是
该区间的末日因子 —— 这一条是后面「用 qfq × 末日因子重建 hfq」的前提。

### 二、测法（每一条都是为了少一个变量）

1. **只改口径，别的一律不动。** 用一个 provider 装饰器把四个价格字段乘上**每票各自的
   常数**，量与额**刻意不乘**（复权本来就不动成交量）。所以「qfq → hfq」在这个实验里
   恰好就是「每只票的价格乘一个正数」——没有换表、没有换数据、没有换策略。
2. **重建先自检。** 基准因子取该票 **sync 区间末日**的因子，然后验算
   `raw × f / base` 是否**逐点**等于库里存的 qfq。第一版把区间取成了回测区间
   （2024-01-02~2026-06-30），自检立刻挡下来：`000004.SZ` 在 20240102 重建
   16.14、库里 14.5578，差 9.8% —— **是分母取错了**，不是数据错。改成按票取
   「不晚于它最后一根 K 线的最大因子日」之后，19/20 只票逐点通过（阈值 1e-6），
   剩下一只 `000004.SZ` **逐条列出并剔除**（不静默跳过 —— 静默跳过会让「20 只票的
   结论」其实只基于 3 只）。
3. **四腿对照**：qfq / hfq × 1M / 100M 资金。
4. **一条均匀缩放腿**：所有票同乘一个常数（取基准中位数 **9.04**），量级与 per-symbol
   那组相当。这条腿是**区分两类通道的关键**。
5. **确定性先验证**：同一命令重跑，四个数逐位相同（`-22.8334 / -1.2555 / -2.1122 /
   -26.9945`）。否则「口径差异」与「跑一次一个样」分不开。

### 三、结果

| 腿 | 收益 | 夏普 | 成交 | 累计股数 |
|---|---|---|---|---|
| qfq @1M | **-22.8334%** | -0.3445 | 235 | 4,880,600 |
| hfq @1M | **-1.2555%** | +0.1038 | 206 | 496,200 |
| 均匀×9.04 @1M | -23.9404% | -0.3820 | 230 | 523,000 |
| qfq @100M | **-2.1122%** | +0.0094 | 245 | 490,120,800 |
| hfq @100M | **-26.9945%** | -0.4033 | 242 | 51,799,600 |

- **口径差异 = 21.58 个百分点**（1M）、**24.88**（100M）。
- **均匀缩放差异 = 1.11 个百分点** —— 与 per-symbol 的 21.58 差了一个数量级，
  而两条腿的**量级完全相同**，只差「是否一致地缩放」。

### 四、结论（**判据被推翻的部分，以及没被推翻的部分**）

**被推翻的**：P2-8 原记「qfq 与 hfq 只差一个每股常数 → 收益率序列完全相同 →
前视只落在价格水平上，**可能**的实质影响只有按股数下单的取整」。前半句是对的
（`tushare_hfq_test.go` 把「收益率逐点相同」和「只差一个常数」都钉成了断言），
**后半句的两个数量级都错了**：

- 差异不是「取整级」的，是 **21.6 个百分点**级的；
- 而且**通道不在取整上**。两条独立证据：
  ① 资金放大 100 倍（经济上只影响整手取整）之后，差异**没有收缩**（21.58 → 24.88）；
  ② 均匀缩放同量级常数只差 1.11pp，per-symbol 差 21.58pp。
  整手粒度对均匀缩放同样敏感，所以它不是主因。**通道在「把不同票的价格放在一起
  比大小」的环节上。**

**上一轮我写下的「最像的嫌疑人」是错的，已被自己的测试推翻。** 当时怀疑
`detectRegime`（它把**所有票、所有日子**的价格拼成一根长序列再算均线 / 波动），
并明确标注「尚未插桩证实」。现在证实它**不是**通道 ——
`pkg/risk/regime_scale_invariance_test.go` 实测：regime 判定对**统一缩放**严格不变
（14/14 窗口），对**每票各自缩放**同样 14/14 窗口完全一致。数学上也成立：
`detectTrend` 用 `fastMA/slowMA` 之比、斜率除以均价、波动率用对数收益，全部对组内缩放不变。

**真正的通道在下单侧，已定位到具体代码行**（下表是本轮定位的原始记录；**AUD-55 已于
2026-09-24 修复** —— 抵扣改成无条件、已持仓改为实时读 tracker，见该条目的 ✅ 段）：

| 环节 | 位置 | 事实 |
|---|---|---|
| 策略无状态 | `pkg/strategy/examples/momentum.go:219` | 对 top-N **每天**发 `Long`，从不发 `Close`；`portfolio` 只用来取日期（`:111`），**不读持仓** |
| 成交后归零 | `engine_daily.go:616` | 全额成交后 `PendingQty = TargetQty − ActualQty = 0` |
| **抵扣条件写窄了** | `engine_daily.go:399` | **只在 `PendingQty > 0` 时**才做「目标 − 已持」抵扣 |
| → 后果 | | `PendingQty == 0`（恰好达标）时抵扣不生效 → 次日按**全量目标**再买一遍；仓位超目标后 `PendingQty < 0`，抵扣**依然**不生效 → **每天重发一次全量买单、每天被 `insufficient cash` 拒一次** |

**这条解释力覆盖当时全部四个观测**（下面是**修复前**的读法；修复后逐条复核过 ——
✅ = 仍然成立，⟳ = 已随修复失效）：

- ✅ **拒单是常态、且不随资金消失**：合成 20 票场景里 1e6~1e10 **每一档**都是 777~1005 笔；
  而 1e10 时**抬一手为 0 只、相对量化仅 3e-5** —— 拒单既与量化无关、也与价格水平无关，
  它就是「重复下单撞上现金上限」。（修复后**全阶梯零拒单**，归因成立。）
- ✅ **每票各自缩放 ≫ 均匀缩放**（合成 4.22pp vs 0.95pp；真数据 21.6pp vs 1.1pp）：
  `NormalizeOrderQuantity` 把不足一手的订单**抬到一手**（`100 × 价格`），这笔金额与仓位
  预算脱钩。每票各自缩放只把**高基准的那几只**推出可交易集合 → **票池成分变了**；
  均匀缩放是全体一起变，成分不变、只改严重程度，所以差一个数量级。
  （修复后这条**更干净**：10.31pp vs 2.60pp —— 修复前那个缺陷把它**冲淡**了。⚠️ 措辞也已
  更正：当时把它写成「重复下单的副产物」，**现在归因为「绝对金额约束」本身**，
  见 AUD-53 的 ✅ 段。）
- ⟳ **只改资金也大幅改变结果**（4.82pp）：当时读作「现金上限是路径变量，改资金即改
  『哪些重复下单能成交』」。**修复后 0.76pp 且阶梯收敛** —— 这条是重复下单的**独有**后果，
  随修复消失；它当时成立，恰恰是「AUD-55 就是根因」的旁证。
- ⟳ **不随资金收敛**（1e8→1e9 差 5.17pp、1e9→1e10 差 8.80pp）：当时读作「结果在同一条
  混沌轨道的不同取法之间跳，彼此没有收敛关系」。**修复后跳幅 0.0364 / 0.0004 pp，收敛** ——
  「路径混沌」这个诊断当时是对的，但**混沌源是 AUD-55，不是系统固有的**。

**边界（没做的那一步）**：以上是「根因已定位到代码行」，**不是「修掉它 21.6pp 就会消失」**——
后者需要改完再实测，尚未做。另：这条缺陷本身与 AUD-53 不是一回事（AUD-53 = 结果对水平/资金
不稳健；重复下单 = 订单逻辑缺陷），故独立登记为 **AUD-55**。

**但 AUD-53 的定性结论不因此改变，反而更硬**：同一套机制对**任何**零经济意义的扰动都这么
敏感（**只改资金**就让 qfq 自己的收益动了 **20.7 个百分点**，且不收敛），所以
**「21.6 个百分点」不是一个稳定的估计量，不能当成「前视偏差值 21.6 个百分点」读**。

### 五、hfq 落不落：判据换过了（2026-09-24）

原计划（`guides/data-dependencies.md`）是「加表 + 落 hfq + 重跑一次同步」。当时
**有意不做**，理由是「先修 AUD-55（重复下单），否则落完拿到的是另一个同样不稳的数」。

**AUD-55 已修**，那条理由用完了 —— 但结论**没有**回到「可以落了」，而是换成了两条更实
的理由：

- hfq 价 = qfq 价 × **每股各自的**常数（实测 1.7~181），**单位已经不是「元」**。
  拿固定资金去交易被放大过的价格 ⇒ 高价票连一手都买不起（合成里 9/20 只被抬到一手、
  最大超买 ×68）⇒ **票池成分被改写**；
- 这条差异**不是引擎缺陷**（一手 = 100 × 价格 是交易所规则），**修复也消不掉它**。

所以「落 hfq」必须连带回答两个问题：**资金要不要按同一基准缩放？** 以及
**「票池被改写」要不要接受？** 不回答这两个，落下去拿到的仍是「与 qfq 差十几个百分点、
而且差在票池上」的另一个数，只是这次它是**确定性的、可解释的**。

**因此 21.6pp 不能读成「前视偏差值 21.6 个百分点」** —— 它里面含着一块与「前视」无关的
成分（票池成分变化）。要估「前视成分」得用**同时缩放资金**的对照：见
`pkg/backtest/scale_invariance_probe_test.go` 的第五条腿（尺度不变腿，差 0.09pp），
那条腿才是「同一策略、同一批票、单位真正一致」的可比基准。

**未做**：真库上按这个新判据重跑一遍（本地无真库）。

### 六、破坏验证

把 hfq 腿的常数去掉（让它与 qfq 腿完全同源）→ **两条断言同时变红**，且指名原因
（「收益差只有 0.0000 个百分点 —— 与实测记录（约 21.6）不符」「均匀缩放 1.1070 与
per-symbol 的 0.0000 同量级」）；`go build ./...` / `go vet ./...` /
`internal/repoguard` 全程绿。反向还原后重跑回到绿。

⚠️ 这一节记的是 **AUD-55 修复前**的真库破坏验证。那两条断言的**文案已按修复后重写**
（不再引用具体数值，只钉方向），但**尚未在修复后的真库上重跑**（本地无真库）。
合成那一套的破坏验证见「七」末尾。

### 七、合成取证（真数据上拆不开的东西，用合成数据拆）

真库那套每跑一次几分钟，且两个现象叠在一起没法分离。补了一个**毫秒级、无网络、无库**的
合成矩阵（`pkg/backtest/scale_invariance_probe_test.go`，20 票 × 520 日，
**收益率序列逐点相同、只有价格水平与资金不同**）：

下表左右两列是 **AUD-55 修复前后**在同一套合成数据上的读数 —— 这个对照本身就是本轮
最重要的取证：

| 腿 | 修复前收益 | 修复后收益 | 与基线差（前 → 后） |
|---|---|---|---|
| 水平×1 / 1e6（基线） | +7.08% | −0.0702% | — |
| 水平×9.04（统一）/ 1e6 | +8.03% | +2.5310% | 0.95pp → **2.60pp** |
| 水平×1.7~181（每票）/ 1e6 | +11.30% | −10.3778% | 4.22pp → **10.31pp**（hfq 腿的真实形状） |
| 水平×1 / 1e8（只改资金） | +2.26% | +0.6876% | 4.82pp → **0.76pp** |
| 水平×9.04、资金×9.04（尺度不变） | 未测 | +0.0215% | — → **0.09pp** |
| 拒单（各腿） | 847~1856 | 0 / 122 / 215 / 0 / 0 | |

三条读法（都不再需要「跨票比大小」这个假设）：

1. **只改资金的差塌了**（4.82 → 0.76pp），资金阶梯转为收敛、基线腿与只改资金腿
   **零拒单** ⇒ AUD-55 就是这条通道。
2. **新增的尺度不变腿差 0.09pp、成交逐笔一致**（价格与资金同比例缩放 ⇒ 股数与整手都
   不变）⇒ 引擎在真正的不变量下确实是不变量；剩下几条腿的差异因此可以归因到
   **绝对金额约束**（一手 = 100 × 价格、最低佣金这类绝对费用）。
3. **每票各自缩放的差反而变大**（4.22 → 10.31pp）—— 不是回归：那条腿只把高基准的票
   推出可交易集合（9/20 抬到一手、最大超买 ×68），而修复前的重复下单缺陷恰好把这条
   效应**冲淡**了（让买得起的票买过头、收益虚高）。10.31pp 是更真实的数字。

**真数据上那两个「互相矛盾」的观测**（统一缩放 ≈ 1.1pp、只改资金 ≈ 20.7pp）在这套合成
数据上复现了 —— 后者在修复后不再显著，前者仍在；两个数的**性质不同**，这也正是 AUD-53
要分开写的原因。

三条护栏（都在 `pkg/backtest/scale_invariance_probe_test.go`）：

- `TestBacktestSensitivityToPriceLevelAndCapital` —— 五腿矩阵 + 断言 1~5，其中断言 5 是
  新增的**归因断言**（尺度不变腿必须 ≪ 「只缩放价格」的腿）。
- `TestBacktestCapitalLadderConverges`（原 `TestBacktestReturnsDoNotConvergeWithCapital`）
  —— 资金阶梯 1e6→1e10。**修复前**：跳幅 **4.38 / 0.44 / 5.17 / 8.80** pp、**不收敛**、
  每档拒单 777~1005。**修复后**：跳幅 **0.7629 / 0.0051 / 0.0364 / 0.0004** pp、
  **收敛**、**全阶梯零拒单**。函数名随命题一起翻转（注释里保留了两轮改名的理由，
  免得后人以为当初判断草率）。
- `TestEngineDoesNotRebuySymbolsThatStayInTarget`（原 `...Rebuys...`）—— 3 只票一直留在
  top-N、**把出场关掉**（止盈乘数拉到 1e6）后：**修复前**每票被买 **5~6 次**、拒单 103 笔、
  运行期间零平仓；**修复后**每票恰好 **1 次**、拒单 **0** 笔。
  （第一版没关出场，读数被「买→止盈→再买」污染成 38~40 笔；关掉后仍是多次买入，
  才说明问题在入场侧而不是出场侧 —— 这个对照是必要的。）

**AUD-55 的破坏验证**（2026-09-24 重做，破坏点 = `computeEffectiveTarget` 的抵扣条件改回
「`PendingQty <= 0` 就返回全量目标」）：**三条护栏同时变红**，各自指名自己的原因 ——
① `TestEngineDoesNotRebuySymbolsThatStayInTarget`：每票 **5 笔 / 6 笔 / 6 笔**（期望 1 笔）、
拒单 **103**；② `TestBacktestCapitalLadderConverges`：最大跳幅 **12.2023** pp、高端残差
**7.3265** pp、每档拒单 **754~1002**；③ 矩阵测试：只改资金 **3.4557** pp（超上界 1.5）、
尺度不变腿 **1.4730** pp（超上界 0.5），且**每条腿都重新出现成批拒单**（964~1856）。
还原后（`grep SABOTAGE` 零残留 + `go build ./...`）三条回到绿。

> ⚠️ 破坏态的数字（拒单 754~1002）与「修复前完全未改」的 777~1005 略有差别，因为破坏
> 验证只还原了**抵扣条件**这一处，`reconcileTargetPosition` 的三处调用仍在 —— 它会让
> `tp.PendingQty` 更贴近真实，于是旧条件「`PendingQty > 0`」通过得多一点。两套数字都
> 成立，不要拿它们互相校对。

---

## 已冻结（本次定位重构后不再投入）

- 多 agent 投票 / arbitrator 仲裁机制 —— 错误相关，放大偏差且不可解释
- 演化算法直接产出交易信号 —— 产出落在"高统计/无因果"象限，不会被采用（改为漏斗顶部的候选生成器）
- EquityDeep 作为"独立工作面"—— 改为数据底座（产业链图谱）+ 研究洞察库

---

_完成一项删一项。本文件超过 5 页 = 该拆分项目了。_
