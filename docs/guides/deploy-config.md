---
status: evergreen
type: how-to
last-verified: 2026-09-18
verified-by: P1-8 收口时逐项核对 docker-compose.yml / deploy/k8s/*.yaml / cmd/{analysis,data}/setup.go
---

# 部署配置：改哪里

> 本地开发走 `docker-compose.yml`，生产走 `deploy/k8s/`。**两套独立维护**，
> 这是有意的（不引入 Kompose / Helm 那套生成链：为几处配置给单人自托管项目
> 加一整套工具，收益不抵复杂度）。代价是可能漂移，所以有两道护栏。

---

## 护栏

1. `python tools/check_deploy_consistency.py` —— 比对两边的关键项
   （服务端口、`DATA_SERVICE_URL`）。CI 里跑，改配置后本地也请跑一次。
2. 关键项 drift 时它会报错并说明差异，不是静默通过。

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

## 服务清单差异（有意的，不是漂移）

| 服务 | compose | k8s | 说明 |
|---|---|---|---|
| postgres | ✅ | 用 `postgres-statefulset.yaml` | 有状态，k8s 不用 Deployment |
| redis | ✅ | 用 `redis-deployment.yaml` | 同上 |
| data-service | ✅ | ✅ | |
| analysis-service | ✅ | ✅ | |
| strategy-service | ✅ | ❌ | standby per ADR-012，k8s 不部署 |

差异清单写在 `tools/check_deploy_consistency.py` 的 `ALLOWED_MISSING_IN_K8S` 里。
**新增差异必须同步加进去并写明原因** —— 否则护栏会报漂移。
