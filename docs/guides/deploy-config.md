---
status: evergreen
type: how-to
last-verified: 2026-09-21
verified-by: P1-8 收口时逐项核对 docker-compose.yml / deploy/k8s/*.yaml / cmd/{analysis,data}/setup.go；AUD-13 补端口绑定约定并实测
---

# 部署配置：改哪里

> 本地开发走 `docker-compose.yml`，生产走 `deploy/k8s/`。**两套独立维护**，
> 这是有意的（不引入 Kompose / Helm 那套生成链：为几处配置给单人自托管项目
> 加一整套工具，收益不抵复杂度）。代价是可能漂移，所以有三道护栏。

---

## 护栏

1. `python tools/check_deploy_consistency.py` —— 比对两边的关键项
   （服务端口、`DATA_SERVICE_URL`）。CI 里跑，改配置后本地也请跑一次。
2. 关键项 drift 时它会报错并说明差异，不是静默通过。
3. 同一个脚本还查一条**单边**规则（AUD-13）：postgres / redis 的端口映射
   必须绑回环。它不比对两边，而是禁止 compose 侧出现「本不该有的暴露」。

---

## 改一个服务的端口，要动这些地方

以 data-service 从 8081 改成 8082 为例：

| # | 文件 | 改什么 |
|---|---|---|
| 1 | `config/data-service.yaml` | `server.port: 8082`（**代码实际读的是这个**） |
| 2 | `docker-compose.yml` | 端口映射 `"8082:8082"` + healthcheck 里的端口 |
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

## 端口绑定：谁绑回环，谁绑全接口（AUD-13）

`docker-compose.yml` 的端口映射有两种写法，差别是**监听哪个网卡**：

| 写法 | 监听 | 用途 |
|---|---|---|
| `"5432:5432"` | `0.0.0.0`（全部网卡） | 局域网内其他机器也能连 |
| `"127.0.0.1:5432:5432"` | 只回环 | 只有本机能连 |
| `"5432"` | 容器端口，宿主端口随机 | 用不到，别写 |

**约定**：

- **postgres / redis 绑回环**。它们是内部依赖，不需要被外部访问；Redis 尤其
  如此 —— 本仓库的 Redis **没有 `requirepass`**，绑 `0.0.0.0` 等于把无鉴权缓存
  交给整个局域网，绑回环是当前唯一有效的访问控制。
- **应用服务（data-service / analysis-service / strategy-service）有意保持
  `0.0.0.0`** —— 它们本来就是要被访问的。

绑回环**不会**打断本机开发：容器之间走 compose 网络的服务名（`postgres:5432`），
与本映射无关；宿主机上的 `psql` 和测试 DSN（`postgres://…@localhost:5432/…`）
照旧通 —— `localhost` 就是 `127.0.0.1`。所以这是「收紧到本机」而不是「关掉」。

> 验收方式（不是看配置文件，是实测）：从**另一个容器**经宿主**局域网 IP**
> 探测，5432/6379 应不通，而绑 `0.0.0.0` 的服务（data-service:8081）应通
> —— 后者是对照组，证明测法本身有区分度。
> ⚠️ 不要用 `host.docker.internal` 做这个测试：它是 Docker Desktop / Rancher 的
> **宿主侧代理**，转发到 loopback，会**绕过网卡绑定**，必然「通」，结论无效。

---

## 服务清单差异（有意的，不是漂移）

| 服务 | compose | k8s | 说明 |
|---|---|---|---|
| postgres | ✅ | 用 `postgres-statefulset.yaml` | 有状态，k8s 不用 Deployment |
| redis | ✅ | 用 `redis-deployment.yaml` | 同上 |
| data-service | ✅ | ✅ | |
| analysis-service | ✅ | ✅ | |
| **web** | ✅ | ✅ | AUD-32 新增，前端（见下节） |
| strategy-service | ✅ | ❌ | standby per ADR-012，k8s 不部署 |

差异清单写在 `tools/check_deploy_consistency.py` 的 `ALLOWED_MISSING_IN_K8S` 里。
**新增差异必须同步加进去并写明原因** —— 否则护栏会报漂移。

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
