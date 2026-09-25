---
status: evergreen
type: how-to
last-verified: 2026-09-25
verified-by: 2026-09-25 部署形态改为「数据库/缓存原生 + 服务容器」后逐项复核 ——
  docker-compose.yml 已无 postgres/redis 服务、config/*.yaml 口径改为宿主机视角、
  check_deploy_consistency.py 的检查 3/5 已改写并跑过 6 项破坏验证（改坏必变红、还原必回绿）、
  容器经 host.docker.internal 连原生 5432/6379 实测可达（并带 localhost 反证腿）
---

# 部署配置：改哪里

> 本地开发走 `docker-compose.yml`，生产走 `deploy/k8s/`。**两套独立维护**，
> 这是有意的（不引入 Kompose / Helm 那套生成链：为几处配置给单人自托管项目
> 加一整套工具，收益不抵复杂度）。代价是可能漂移，所以有三道护栏。
>
> **2026-09-25 形态变更**：本地开发侧，**数据库与缓存改为宿主机原生安装**
> （PostgreSQL 17.5 / Redis 7.4.11，由 `tools/local-infra.sh` 启停），
> **应用服务仍然全部跑在容器里**。k8s 侧不变（postgres-statefulset +
> redis-deployment 保留 —— k8s 本就不该依赖宿主机数据库）。

---

## 地址口径：三处不同，且都对（这是最容易踩的点）

同一批数据库/缓存，在三个地方写的地址**必然会不一样**，这不是漂移：

| 位置 | DATABASE_HOST | 为什么 |
|---|---|---|
| `config/*.yaml` | `localhost` | 在**宿主机上直接跑**时的视角 |
| `docker-compose.yml` | `host.docker.internal` | 容器内指**宿主机**；容器里的 `localhost` 是容器自己 |
| `deploy/k8s/configmap.yaml` | `postgres` | k8s 里的 Service 名 |

**服务之间的地址是另一回事**：`http://data-service:8081` 这类保持 compose 服务名 /
k8s Service 名不变 —— 服务全在容器里，这一跳没有变。不要跟着数据库一起改成
`localhost`（宿主机的 8081 上没有 data-service）。

---

## 护栏

1. `python tools/check_deploy_consistency.py` —— 比对两边的关键项
   （服务端口、`DATA_SERVICE_URL`）。CI 里跑，改配置后本地也请跑一次。
2. 关键项 drift 时它会报错并说明差异，不是静默通过。
3. 同一个脚本还查一条**单边**规则（AUD-13，2026-09-25 改写）：数据库/缓存
   **不得由 compose 托管**，且应用侧必须显式指向 `host.docker.internal`。
   原先这条守的是 compose 的端口映射，服务移出 compose 后**检查对象不存在了** ——
   不改写就等于让检查 3 空转（循环体一次都不进，脚本照旧打「✓」），那是假护栏。
   原生进程的 `listen_addresses` / `bind` 由 `tools/local-infra.sh status` 断言
   （它读真实的 netstat 监听 socket）。
4. **检查 3c（2026-09-25 新增，AUD-52）**：open-access 的声明与端口发布范围
   **互相蕴含**。容器必须绑 `0.0.0.0` 才能被发布端口转发，于是 P0-4 那道
   「open-access 只许在 loopback 上」的门判不了绑定地址，只能判**发布层** ——
   `cmd/analysis` 因此承认一个显式声明 `AUTH_INSECURE_EXPOSURE=loopback-published`。
   声明本身不做保证，保证是这一项给的，四条双向规则：

   | # | 条件 | 要求 |
   |---|---|---|
   | 3c-1 | `AUTH_INSECURE` 为真 | **每一条** ports 映射都必须带 `127.0.0.1:` 前缀 |
   | 3c-2 | 存在非回环映射 | 必须有非空 `JWT_SECRET`（对外发布必须配鉴权） |
   | 3c-3 | `AUTH_INSECURE` 为真 | `AUTH_INSECURE_EXPOSURE` 必须是 `loopback-published` |
   | 3c-4 | 同时给 `JWT_SECRET` 与 `AUTH_INSECURE=true` | 报错（密钥优先，声明会误导读者） |

   运行时那一半在 `tools/local-stack.sh status`（读真实 netstat）。两者缺一
   不可：静态管不到运行时改动与 override 文件，运行时管不到未来还没起的那次
   提交。决策与备选方案见 [ADR-025](../adr/adr-025-auth-exposure-publish-layer.md)。
   **只写一个方向会漏掉一半** —— 3c 的破坏验证是 5 个用例（4 红 + 1 绿对照）。

5. **检查 5 已放宽**（2026-09-25）：`compose ↔ k8s` 只比**库名 / 用户名 / 端口**，
   **不比 host 与 URL** —— 理由见上面的「地址口径」表：host 在三处必然不同，
   比它只会逼出一个恒假的断言。`config/*.yaml` 的 host 同理不参与比对。

---

## 改一个服务的端口，要动这些地方

以 data-service 从 8081 改成 8082 为例：

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `config/data-service.yaml` | `server.port: 8082`（**代码实际读的是这个**） |
| 2 | `docker-compose.yml` | 端口映射 `"127.0.0.1:8082:8082"`（回环前缀不能丢，见检查 3c）+ healthcheck 里的端口 |
| 3 | `deploy/k8s/data-deployment.yaml` | `containerPort`（Deployment）与 `port`（Service） |
| 4 | 任何 `DATA_SERVICE_URL` | compose 的 environment + `deploy/k8s/configmap.yaml` |

第 4 步最容易漏：它由护栏兜住。

---

## 两个反直觉的点

**端口不来自环境变量。** 代码读的是 viper 的 `server.port`
（`cmd/analysis/setup.go`），来自 `config/*.yaml`。所以
`ANALYSIS_PORT` / `DATA_PORT` 这类环境变量**从未被读取** —— 它们曾在
configmap 里躺了很久，2026-09-18 作为死配置清掉（同 P2-7 的 ai-service.yaml）。
想用环境变量覆盖要用 viper 的键名：`SERVER_PORT`。

**`DATA_SERVICE_URL` 是被读的，别以为有默认值就不用配。**
它对应 viper 的 `data_service.url`（`AutomaticEnv` + `.`→`_` 替换），
`cmd/analysis/handlers_proxy.go` 里读。代码有默认值 `http://data-service:8081`，
所以 k8s 里长期缺这一项也"能用" —— 但那只是因为 k8s 的 Service 名恰好也叫
`data-service`、端口恰好也是 8081。Service 一改名就静默失效。现在显式配了。

---

## 端口绑定：全都只绑回环（AUD-13 的推广）

**2026-09-25 起这条约定的落点变了**，而且覆盖面**扩大了**：数据库/缓存不再是
compose 服务（端口映射这个载体不存在了），同时**应用服务也从「有意绑 0.0.0.0」
改成了「只发布到回环」**。现在的分布是：

| 目标 | 由谁保证 | 怎么做 |
|---|---|---|
| 数据库/缓存只绑回环 | **原生进程的启动参数** | `tools/local-infra.sh` 用 `-c listen_addresses=127.0.0.1` / `--bind 127.0.0.1` **显式**指定，不靠配置文件默认值 |
| 数据库/缓存只绑回环（**验证**） | `tools/local-infra.sh status` | 读 **netstat 的真实监听 socket**，出现非回环就报 `✗` |
| 应用服务只发布到回环 | `docker-compose.yml` 的四条 `ports` | **全部**带 `127.0.0.1:` 前缀 |
| 应用服务只发布到回环（静态检查） | `check_deploy_consistency.py` 检查 3c | `AUTH_INSECURE` 为真 ⟹ 每条映射都必须回环 |
| 应用服务只发布到回环（**验证**） | `tools/local-stack.sh status` | 读 netstat 断言 8080/8081/8082/8085 只绑回环 |

**为什么验证读 netstat 而不是配置文件**：配置文件可以被命令行参数覆盖 ——
这个脚本自己就是靠 `-c` / `--bind` 覆盖的。只有 socket 是 ground truth。

**约定**：

- **数据库/缓存只绑回环**。它们是内部依赖，不需要被外部访问；Redis 尤其
  如此 —— 本仓库的 Redis **没有 `requirepass`**，绑 `0.0.0.0` 等于把无鉴权缓存
  交给整个局域网，绑回环是当前唯一有效的访问控制。
- **应用服务（data-service / analysis-service / strategy-service / web）也只
  发布到回环**，因为 analysis 以 **open-access** 运行（无鉴权，见
  [ADR-025](../adr/adr-025-auth-exposure-publish-layer.md)）：容器**必须**绑
  `0.0.0.0` 才能被转发，于是「不可从其他主机到达」这条不变量只剩下发布层这一个
  着力点。写成 `"8085:8085"` 等于把下单接口开给整个局域网。
- 要给局域网设备访问时，**两件事一起做**：去掉 `127.0.0.1:` 前缀 + 设
  `JWT_SECRET`。只改一半会被检查 3c 拦下。
- 检查 3c 认的回环写法是 `127.*` 前缀、`::1` / `[::1]`、`localhost`；**不写宿主
  地址**（裸 `"8085:8085"`）一律当成绑 `0.0.0.0`，与运行时 netstat 读到的
  `0.0.0.0:8085` 一致 —— 两边对「非回环」的定义刻意对齐。

验收方式（实测，三条一起看才完整）：

```bash
# ① 容器必须能到宿主机（这是新形态的**必需能力**，不是漏洞）
docker run --rm alpine:3.20 nc -z host.docker.internal 5432   # 应通
# ② 原生进程自身只监听回环（局域网其他机器连不上）
netstat -ano | grep LISTENING | grep 5432                      # 应只见 127.0.0.1
# ③ 容器服务的宿主侧映射也只绑回环（一条命令顶上面整张表）
tools/local-stack.sh status                                    # 四个端口都应 ✓
```

> ⚠️ **别用 `host.docker.internal` 去判断「绑定是否收紧」** —— 它是 Docker
> Desktop 的**宿主侧代理**，转发到 loopback，会**绕过网卡绑定**、必然「通」。
> 拿它对绑定下结论是无效的。那个问题由 `local-infra.sh status` / `local-stack.sh
> status` 读 netstat 回答。

---

## 服务清单差异（有意的，不是漂移）

| 服务 | compose | k8s | 说明 |
|---|---|---|---|
| postgres | ❌ **宿主机原生** | 用 `postgres-statefulset.yaml` | 2026-09-25 移出 compose（见文件头）。k8s 侧不变 |
| redis | ❌ **宿主机原生** | 用 `redis-deployment.yaml` | 同上 |
| data-service | ✅ | ✅ | |
| analysis-service | ✅ | ✅ | |
| **web** | ✅ | ✅ | AUD-32 新增，前端（见下节） |
| strategy-service | ✅ | ❌ | standby per ADR-012，k8s 不部署 |

差异清单写在 `tools/check_deploy_consistency.py` 的 `ALLOWED_MISSING_IN_K8S` 里。
**新增差异必须同步加进去并写明原因** —— 否则护栏会报漂移。
`postgres` / `redis` 两条已从该清单删除（2026-09-25）：它们不再是 compose 服务，
留在清单里就是死条目 —— 「不许回到 compose」改由检查 3a 正向断言。

---

## 前端 web 服务（AUD-32）

`web/`（Vue 3 + Vite）是官方前端，此前**没有任何部署** —— 只能 `npm run dev`
跑在 `:5173`，compose 里连一个服务都没有。这让 `cmd/analysis/static/` 那套
legacy HTML 成了唯一的服务端 UI，想删也删不掉 —— 这个服务补上之后，
AUD-33 才把 legacy 删掉（2026-09-22）。**现在 SPA 是唯一前端。**

现在是 compose / k8s 里的一个 `web` 服务，**宿主端口 8080**：

```
浏览器 ──:8080──▶ nginx（web 容器）
                    ├─ /api/*  ──反代──▶ analysis-service:8085
                    └─ 其余    ────────▶ dist/index.html（SPA history 回退）
```

### 三个「为什么这么接」

**① 由 nginx 反代 `/api`，而不是让前端直连 `:8085`。**
前端的 API 基址默认是空字符串（`web/src/api/client.ts:65`，
`import.meta.env.VITE_API_BASE || ''`），所有请求走相对路径 `/api/...`。
反代之后浏览器看到的仍是同源的 `/api/...`，**前端一行都不用改，CORS 也不用动**
—— `SERVER_CORS_ALLOWED_ORIGINS` 现在是留空的（= 不回显任何 ACAO，最安全），
同源请求根本不触发 CORS。若改成前端直连 `:8085`，就得把 `http://<host>:8080`
加进白名单：把一个「最安全」的默认值换成「有一个源被放行」，只为省一层反代。

**② nginx 里用 `resolver` + 变量写反代目标，不写死域名。**
`proxy_pass http://analysis-service:8085;` 这种写法，nginx 只在**启动时**解析一次
并缓存到进程退出 —— 后端晚起来会让 nginx 直接启动失败，后端重启换 IP 后 nginx
仍在打旧地址。变量 + `resolver` 让它每次请求时解析（见 `deploy/nginx-spa.conf`
里的注释）。resolver 写了两个地址：Docker 的 `127.0.0.11` 与 k8s CoreDNS 默认的
`10.96.0.10` —— **k8s 集群若改过 service CIDR，这个地址要跟着改**。

**③ SSE 必须显式关掉缓冲。**
同步进度用的是 `EventSource`（`web/src/stores/sync.ts:162`）。nginx 默认缓冲上游
响应，SSE 事件会攒在缓冲区里不往下发 —— 前端表现是「进度条一动不动，等任务跑完
才一次性跳到 100%」。要三件事一起做：`proxy_buffering off` + 放大
`proxy_read_timeout` + 给外层中间层发 `X-Accel-Buffering: no`（k8s 里
ingress-nginx 在本机 nginx 之前，只关自己这层不够）。

### ⚠️ web 是「改端口要动 4 个地方」的例外

上面那张表的第 1 步（`config/*.yaml` 的 `server.port`）对 web **不适用** ——
它是 nginx 不是 Go 服务，端口来自 `deploy/nginx-spa.conf` 的 `listen`。
改 web 的端口要动：

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `deploy/nginx-spa.conf` | `listen 8080` + healthcheck 无关 |
| 2 | `Dockerfile.web` | `EXPOSE 8080` |
| 3 | `docker-compose.yml` | 端口映射 + healthcheck 里的端口 |
| 4 | `deploy/k8s/web-deployment.yaml` | `containerPort`（Deployment）与 `port`（Service） |
| 5 | `deploy/k8s/ingress.yaml` | backend 的 `number: 8080` |

**第 5 步没有护栏兜** —— `check_deploy_consistency.py` 只比对 compose 与 k8s
Service 的端口，不管 ingress 的 backend。**这条漏改不会报错，只会让 k8s 路径下
前端打不开。**

### 未实证的部分

compose 侧已实证（构建 + 跑容器 + curl 验证）。**k8s 侧只做了配置对齐，没有
集群可跑** —— `web-deployment.yaml` 与改动后的 `ingress.yaml` 属于「按同一套
约定推出来的」，首次真跑 k8s 时请重点看：`quant-trading/web:latest` 镜像是否
存在、CoreDNS 地址是否为 `10.96.0.10`、ingress 去掉 `rewrite-target` 后
`/api/*` 是否完整透传。
