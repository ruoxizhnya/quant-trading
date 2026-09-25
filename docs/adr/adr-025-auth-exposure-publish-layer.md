# ADR-025: P0-4 鉴权豁免的判据从「绑定地址」换成「发布层」

> **Status**: Accepted
> **Date**: 2026-09-25
> **Category**: Architecture / Security
> **Related**: [ADR-017](adr-017-observability-and-auth.md)（API 鉴权）· [ADR-019](adr-019-service-merge-ai-copilot.md) · [ODR-021](../archive/odr/odr-021-p1-15-service-merge-risk-execution.md)（risk+execution 并入 analysis）· [TASKS.md](../TASKS.md) AUD-52
> **Upstream**: 用户（若曦）2026-09-25 决策（「两段都修」+「端口仅 127.0.0.1 发布」）

---

## Context

P0-4 立下的不变量是：**open-access 绝不能从其他主机到达**。当时的实现
（`cmd/analysis/setup.go:decideAuthStartup`）用 `server.host` 判它：

```
AUTH_INSECURE=true  且  server.host ∈ {127.0.0.1, localhost, ::1, ...}  → 放行
否则                                                                   → 拒绝启动（fail closed）
```

这个判据在「进程直接跑在宿主上」时是成立的 —— 绑定地址就是暴露面。

2026-09-25 的部署改造（数据库原生 + 服务容器）把这个前提打掉了：
**容器必须绑 0.0.0.0 才能被发布端口转发**。绑 127.0.0.1 的容器，宿主机
从映射端口连不进去（宿主侧的转发目标是容器的 eth0，不是它的 loopback）。

于是出现了两个都不成立的状态：

| 配置 | 结果 |
|---|---|
| `AUTH_INSECURE=true` + 容器绑 0.0.0.0 | 服务**拒绝启动**（判据认为暴露到局域网了） |
| 配 `JWT_SECRET` + 容器绑 0.0.0.0 | 服务起来了，但**整个前端与 e2e 套件全站 401** |

第二条不是小问题。实测（2026-09-25，容器栈）：

```
GET :8085/health                 → 200   ← 公开路径白名单（pkg/auth/middleware.go:221）
GET :8085/api/strategies         → 401   missing Authorization header
GET :8080/api/strategies         → 401   ← 经 nginx 反代，也就是**前端自己**拿不到数据
GET :8080/api/stocks/count       → 401
```

而前端**没有登录页、没有 auth store、`web/src/api/client.ts` 是裸 fetch 不带
token**；系统也**没有首个管理员的引导**（`CreateUser` 只挂在
`RequireRole(admin)` 后面 —— 鸡生蛋）。所以「配了 JWT_SECRET」这个选项在
当前产品形态下等于**把 UI 关掉**：服务"起得来但用不了"。

同时 e2e 套件（160 条 Playwright + `e2e/tests/integration_test.go`）明确假定
open-access（`e2e/tests/rbac-open-access.spec.ts` 开篇即写 "The e2e environment
runs with auth DISABLED"），所以这个矛盾不是「AUD-52 那两条测试」的局部问题 ——
**AUD-52 只是它最显眼的一处症状**。

---

## Decision

### 1. 判据从「绑定地址」换成「发布层」

新增一个**显式声明**（不是自动推断）：

```
AUTH_INSECURE_EXPOSURE=loopback-published
```

`decideAuthStartup` 的放行条件变成二者之一：

- **A** `AUTH_INSECURE=true` 且 `server.host` 是 loopback（原生跑法，原行为不变）
- **B** `AUTH_INSECURE=true` 且 `AUTH_INSECURE_EXPOSURE=loopback-published`
  （容器跑法：绑定是 0.0.0.0，但发布层限定为回环）

取值必须**逐字**等于 `loopback-published`：拼错、大小写不符、写近义词、
写 `true`，一律按「没声明」处理 → 拒绝启动。

**声明本身不提供任何保证。** 保证由两处机器校验给出，声明的作用是让
「有人主动承诺过」这件事在配置里可见、可 review：

1. **静态** —— `tools/check_deploy_consistency.py` 检查 3c，四条双向规则：
   - 开了 open-access → **每一条** ports 映射都必须带 `127.0.0.1:` 前缀
   - 存在非回环映射 → 必须有非空 `JWT_SECRET`（对外发布必须配鉴权）
   - 开了 open-access → `AUTH_INSECURE_EXPOSURE` 必须是 `loopback-published`
   - 同时给 `JWT_SECRET` 与 `AUTH_INSECURE=true` → 报错（密钥优先，声明会误导读者）
2. **运行时** —— `tools/local-stack.sh` 读宿主机真实 `netstat`，断言
   8080/8081/8082/8085 只监听回环（与 AUD-13 对 PG/Redis 的做法同源）。

两者缺一不可：静态管不到运行时改动与 override 文件，运行时管不到未来还没起
的那次提交。

### 2. 本地默认 = open-access；对外发布 = 必须配鉴权

`docker-compose.yml` 的默认值改成：

```yaml
AUTH_INSECURE: ${AUTH_INSECURE:-true}
AUTH_INSECURE_EXPOSURE: ${AUTH_INSECURE_EXPOSURE:-loopback-published}
JWT_SECRET: ${JWT_SECRET:-}          # 不再强制；设了则密钥优先
ports: - "127.0.0.1:8085:8085"       # 四条映射全部回环
```

要给局域网设备访问时，**两件事一起做**：去掉 `127.0.0.1:` 前缀 + 设
`JWT_SECRET`。只改一半会被检查 3c 拦下。

### 3. 套件门与断言同源（AUD-52 的测试侧一半）

`e2e/tests/integration_test.go` 的 `TestMain` 原来只探 `/health`
（「服务在」），而用例要的是「服务能用」。现在判三档，判据就是各用例真正会打
的路径：

| 档 | 判据 | 动作 |
|---|---|---|
| ① | `/health` 不通 | skip（S7-P0-8 原意：环境没起 → 退出 0） |
| ② | 通了但 `/api/execution/*` 404 | **FAIL**（真回归：ODR-021 后的路由必须存在） |
| ③ | 都在但要鉴权（401/403） | skip + 打印怎么改姿势（形态不匹配 ≠ 缺陷） |

同时修掉三处**指向已退役架构**的断言：拨 `:8084`、把 risk 当独立服务
（且 `/risk/health` 这个路径从来没有存在过）、把 `/api/strategies` 的
`{"strategies": [...]}` 响应按裸数组解（导致断言恒空）。

---

## Consequences

**正面的：**

- 基础不变量（open-access 不可从其他主机到达）**没有被削弱**，只是保证的位置
  从「进程绑定」挪到「端口发布」—— 而后者才是容器形态下暴露面的真身。
- 前端与两套 e2e 恢复可用；`go test ./...` 不再有一条永远不可能绿的用例。
- 「诚实」这件事可以验证：检查 3c 与运行时断言都有破坏验证（分别 5/5 与
  实测报红），e2e 门的两档判据也有 5/5 破坏验证。

**代价 / 风险：**

- **多了一个需要维护的配对关系**（`AUTH_INSECURE` ⟺ 全回环发布）。这是本轮
  刻意引入的复杂度，用它换「容器里也能有合法 open-access」。缓解手段就是
  上面那两处机器校验 —— 如果把它们删掉，这个 ADR 就退化成一句口头承诺。
- **本地默认 open-access 意味着「默认不安全」**。这是有意的：默认值面向的是
  单人自托管实验室的本机开发，且暴露面被发布层钉死。要对外必须显式改两处。
- 局域网/手机访问不再是「改一个数字」的事，而要连带配鉴权（而鉴权目前没有
  登录页可配）—— **这是已知的产品缺口**，不在本 ADR 范围内，但它是
  「要不要做登录页」这个议题的直接来源。

## Alternatives Considered

1. **只改测试侧（门里 skip 401），不动安全门。** 最省事，但 dev compose 仍是
   鉴权开启 → 前端与 160 条 Playwright 仍全站 401，UI 实际不可用。等于把
   问题从「测试红」挪成「产品不可用」。**否**。

2. **让 e2e 自己去取 token。** 被设计封死：无首个管理员引导，`CreateUser`
   在 `RequireRole(admin)` 后面。要打开就得先加一个「无认证时允许创建首个
   admin」的引导接口 —— 那是给生产开一个洞。**否**。

3. **容器改 `network_mode: host`。** 那样容器的 loopback 就是宿主的 loopback，
   `server.host=127.0.0.1` 就能满足原判据。但 host 网络在 Docker Desktop 上
   语义特殊（`host.docker.internal` 行为随之变化），且会连带推翻
   `wait-for-deps.sh` 与「服务间用 compose 服务名」这两条设计。**否**（代价
   远大于收益）。

4. **自动推断「我在容器里且只发布了回环」。** 进程看不到自己的 ports 映射，
   只能靠 `/.dockerenv` 之类猜「在容器里」—— 而「在容器里」**不等于**「只发布
   到回环」（`-p 8085:8085` 也是容器）。自动推断会给出虚假的安全感。**否** ——
   这正是选择「显式声明 + 机器校验」而不是「聪明推断」的理由。

5. **把 `/api/strategies` 加进公开路径白名单。** 治标：其它所有 `/api/*` 仍然
   401，前端还是不可用。**否**。
