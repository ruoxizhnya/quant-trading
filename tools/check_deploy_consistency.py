#!/usr/bin/env python3
"""校验 docker-compose.yml 与 deploy/k8s/*.yaml 的关键项是否一致（P1-8）。

背景：本地开发走 docker-compose，部署走 k8s，两套配置独立维护 —— 改了一边
忘了另一边就会漂移，而且漂移往往是**静默**的：服务起得来，只在某个调用
跨服务时才失败（比如 analysis 找不到 data-service）。

本脚本只比对**会互相影响**的关键项：
  1. 服务名 → 端口（公共服务必须一致）
  2. DATA_SERVICE_URL：compose 的 environment 与 k8s configmap 必须一致，
     且它指向的 host:port 要真的等于 data-service 的端口
  3. 数据库/缓存必须来自**宿主机原生安装**，且容器**显式**指向它（AUD-13 改写）。
     2026-09-25 之前这条是「compose 的 postgres / redis 服务端口只绑回环」——
     那两个服务已移出 compose（改由宿主机原生安装），端口映射这个**检查对象
     本身不存在了**。新的等价断言有两半：
       a) compose 里不得再出现 postgres / redis 服务（留着会与原生进程抢端口）；
       b) 应用侧的 DATABASE_HOST / REDIS_URL 必须指向 host.docker.internal。
     后半条接替了被移除的 `depends_on + service_healthy`：容器里的 localhost
     指容器自己，写成 localhost 语法完全合法、启动才失败。
     原生进程的 listen_addresses / bind 由 tools/local-infra.sh 在机器上断言 ——
     绑定的真身在仓外，静态脚本管不到它，写在这里只会变成假护栏。
  4. 部署配置里不得出现 GIN_MODE（AUD-29）。gin mode 的唯一来源是
     config/*.yaml 的 server.gin_mode；GIN_MODE 会被 gin 自己读走，是同一个
     决定的第二个入口，而且 grep 不到读取点。
  5. compose ↔ k8s 的数据库/缓存 env **口径**必须一致，且库名/用户名
     不能与 config/*.yaml 漂移（AUD-39）。
     2026-09-25 **放宽**：地址（DATABASE_HOST / REDIS_URL 的 host）不再比对 ——
     数据库移到宿主机后容器内必然是 host.docker.internal，而 k8s 里是 Service
     名 postgres，两者**必然不同**，比它只会逼出一个恒假的断言。仍然比对：
     库名、用户名、端口，以及密码取自同一个变量。config/*.yaml 的 host 现在是
     宿主机视角（localhost），同样不参与比对。
  6. 注入到应用容器的每个 env 名都必须有读取点（AUD-39）。读取点从 Go 源码
     的 Get*/UnmarshalKey/BindEnv/Sub 调用点推导，不是维护一张手写清单。
  7. config/*.yaml 里不得出现 ${...}（AUD-39）。本仓没有 env 展开器，占位符
     不是模板，是字面量 —— 它曾被当成真的密码 / token 用。
  8. 死键负向钉（AUD-39）：POSTGRES_HOST/PORT、REDIS_HOST/PORT、
     REDIS_PASSWORD、DB_PASSWORD 不许回来；POSTGRES_DB/USER/PASSWORD 只许
     出现在 postgres 容器自己的 env 里。

刻意不做的事：
  - 不做「一份源生成两份」。那要引入 Kompose / Helm 之类的工具，为了
    几处配置给单人自托管项目加一整套生成链，收益不抵复杂度。
  - 不要求服务集合完全相同。k8s 里没部署 strategy-service（standby per
    ADR-012）是**有意的**，写死在 ALLOWED_K8S_ONLY_MISSING 里。

检查 6 的**边界**（写出来，免得被当成全覆盖）：
  - 「env 可达性」的基准是 Go 源码里 `Get*` / `UnmarshalKey` / `Sub` /
    `BindEnv` / `os.Getenv` 的调用点，加上 `ConfigKey* = "a.b"` 这类常量定义。
  - **不覆盖** `viper.Unmarshal(&整个结构体)` 读的键（只能靠结构体 tag 反推，
    代价不划算）。这类键若进了 configmap 会被**误报**，加进
    ENV_NAME_ALLOWLIST 并写明理由 —— 宁可误报，不可漏报。
  - 只查 k8s 的 analysis/data 两个 deployment（应用容器）。postgres / redis
    的 env 是**容器镜像自己的**约定（POSTGRES_*），不由本仓 Go 代码消费，
    因此不参与可达性检查，只参与「不许作为 configmap 键」的负向检查。

只用标准库（这个环境没有 pyyaml），靠正则抓规整的 YAML 缩进块。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
COMPOSE = ROOT / "docker-compose.yml"
K8S_DIR = ROOT / "deploy" / "k8s"

# k8s 里有意不部署的服务（及其原因）。改这个清单本身就该被 review。
#
# ⚠️ postgres / redis 两条已于 2026-09-25 删除：它们不再是 compose 服务
# （改由宿主机原生安装），所以检查 1 根本遍历不到它们 —— 留着是**死条目**，
# 看起来在解释什么、实际永远查不到。「它们不许回到 compose」这件事现在由
# 检查 3a 正向断言。
ALLOWED_MISSING_IN_K8S = {
    "strategy-service": "standby per ADR-012，k8s 里不部署",
}

# 两边都可能不配的最小集合（比如只在本地跑的辅助容器）
IGNORED_SERVICES = {"equitydeep-research"}

# AUD-13（2026-09-25 改写）：数据库/缓存不再由 compose 托管，改由宿主机原生
# 安装提供（PostgreSQL 17.5 / Redis 7.4.11，见 docker-compose.yml 文件头）。
#
# 原来这里有一对常量（LOOPBACK_ONLY / LOOPBACK_BINDS）守的是 compose 的 ports
# 映射 —— 那两个服务已从 compose 移除，**检查对象不存在了**。留着不改的后果不是
# 报错，而是检查 3 空转：循环体一次都不进，脚本照常打「✓ 数据库/缓存只绑回环」。
# 那是典型的假护栏（比没护栏更糟）。
#
# 换成下面这组：守「基础设施确实在仓外」+「容器确实指向它」。
NATIVE_DB_DEPS = ("postgres", "redis")

# 容器内用来指宿主机的名字。**不能是 localhost** —— 容器里的 localhost 指容器
# 自己。实测（2026-09-25）：Docker Desktop 默认把它解析成 IPv4 192.168.65.254，
# 且能连到宿主机回环上监听的服务。
CONTAINER_HOST_ALIAS = "host.docker.internal"

# 每个应用服务**必须显式**注入的基础设施 env：(env 名, 断言方式)。
# 这是本轮改造后的承重项 —— config/*.yaml 现在的口径是宿主机视角（localhost），
# 容器服务一旦漏注入就会静默回落到 localhost 并连到容器自己；compose 语法合法、
# 启动日志也只是一句连接失败。此前这层保证由 depends_on 提供，现在没有了。
INFRA_ENV_REQUIRED: dict[str, tuple[tuple[str, str], ...]] = {
    "data-service": (("DATABASE_HOST", "host"), ("REDIS_URL", "url")),
    "analysis-service": (("DATABASE_HOST", "host"), ("REDIS_URL", "url")),
    "strategy-service": (("REDIS_URL", "url"),),
}


# 端口映射的三种写法都要认：
#   "5432"                  → 只暴露容器端口，宿主端口随机（本仓未用）
#   "5432:5432"             → 绑所有网卡
#   "127.0.0.1:5432:5432"   → 只绑回环
#
# ⚠️ AUD-13 之前这个正则只认两段式。加上回环前缀后若不改它，postgres / redis
# 会**静默**从 compose_svc 里消失 —— 而两者都在 ALLOWED_MISSING_IN_K8S 里，
# 只出 note 不报错，护栏就此失效（「两个机制各自对、接起来就错」）。
# 以后再改端口写法，务必同时验这条正则还认不认。
_PORT_LINE = re.compile(r'^\s*-\s*"?((?:[\w.\-]+):)?(\d+):(\d+)"?\s*$')


def parse_compose_ports(path: Path) -> dict[str, list[tuple[str | None, int]]]:
    """抓 docker-compose 的 service → [(宿主绑定地址 or None, 宿主端口)]。

    绑定地址为 None 表示端口映射没写宿主地址，等价于绑 0.0.0.0。
    """
    bindings: dict[str, list[tuple[str | None, int]]] = {}
    current: str | None = None
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        m = re.match(r"^ {2}([A-Za-z0-9_.-]+):\s*$", line)
        if m:
            current = m.group(1)
            continue
        m = _PORT_LINE.match(line)
        if m and current:
            bind = m.group(1).rstrip(":") if m.group(1) else None
            bindings.setdefault(current, []).append((bind, int(m.group(2))))
    return bindings


def parse_k8s_ports() -> dict[str, int]:
    """抓 k8s Service 的 name → port（Service 才是别的 Pod 访问用的入口）。"""
    ports: dict[str, int] = {}
    for f in sorted(K8S_DIR.glob("*.yaml")):
        text = f.read_text(encoding="utf-8")
        for doc in text.split("\n---\n"):
            if not re.search(r"^kind:\s*Service\s*$", doc, re.M):
                continue
            name = re.search(r"^ {2}name:\s*(\S+)\s*$", doc, re.M)
            port = re.search(r"^\s*port:\s*(\d+)\s*$", doc, re.M)
            if name and port:
                ports[name.group(1)] = int(port.group(1))
    return ports


def find_env_value(text: str, key: str) -> str | None:
    m = re.search(rf'^\s*{re.escape(key)}:\s*"?([^"\n]+?)"?\s*$', text, re.M)
    return m.group(1).strip() if m else None


def find_nested_value(text: str, section: str, key: str) -> str | None:
    """抓 `section:` 块里第一层的 `key: value`（2 空格缩进）。"""
    in_section = False
    for line in text.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        if re.match(rf"^{re.escape(section)}:\s*$", line):
            in_section = True
            continue
        if not in_section:
            continue
        if re.match(r"^\S", line):  # 到了下一个顶层键，section 结束
            return None
        m = re.match(r'^ {2}([a-z_]+):\s*"?([^"\n]*?)"?\s*$', line)
        if m and m.group(1) == key:
            return m.group(2).strip()
    return None


# ── AUD-39：env 口径护栏 ──────────────────────────────────────────────
#
# 这一组针对的是一类**静默**失败：部署配置里注入的 env 名与代码实际读的配置
# 键推导出的 env 名不是同一个字符串。服务照常启动，只在真正用到时才失败，而且
# 失败信息（"password authentication failed"）读起来像「值错了」而不是
# 「名字错了」—— k8s 侧曾同时给错键名（POSTGRES_* vs DATABASE_*）和错值
# （quantlab vs quant_trading）。
#
# 基准是 viper 的推导规则：AutomaticEnv + SetEnvKeyReplacer(".", "_") 把配置键
# `database.host` 映射成 env 名 **DATABASE_HOST**。这条规则一旦改，下面两个检查
# 都要跟着改。

_GO_KEY_CALL = re.compile(
    r"(?:GetString|GetInt|GetInt64|GetBool|GetFloat64|GetDuration|GetStringSlice"
    r"|GetStringMap|GetStringMapString|GetTime|BindEnv)"
    r'\(\s*"([a-z0-9_.]+)"'
)
# viper.UnmarshalKey("trading", …) 覆盖整个子树 → 记成前缀 TRADING_。
_GO_UNMARSHAL_KEY_CALL = re.compile(r'UnmarshalKey\(\s*"([a-z0-9_.]+)"')
# viper.Sub("backtest") 同理。注意 time.Time.Sub 也命中 `.Sub(`，但它的实参
# 不是字符串字面量，不会匹配。
_GO_SUB_CALL = re.compile(r'\.Sub\(\s*"([a-z0-9_.]+)"')
_GO_GETENV_CALL = re.compile(r'os\.Getenv\(\s*"([A-Z0-9_]+)"')
# 有些键是通过常量读的（`v.GetString(httpserver.ConfigKeyGinMode)`），字面量
# 正则抓不到 —— 把常量定义一起收进来，而不是维护一张手写清单。
_GO_CONFIG_KEY_CONST = re.compile(r'ConfigKey[A-Za-z0-9_]*\s*=\s*"([a-z0-9_.]+)"')

# 应用读的数据库/缓存 env 名（5a 用它断言「必备键都在」）。
#
# 注意 5b **不再**拿这个清单去逐一比值：DATABASE_HOST / REDIS_URL 在三处口径
# 不同且都正确 —— config/*.yaml 是宿主机视角（localhost）、compose 是
# host.docker.internal、k8s 是 Service 名 postgres。比值的清单见 5b 里那几个
# 身份字段（库名 / 用户名 / 端口）。
APP_DB_ENV_KEYS = [
    "DATABASE_HOST",
    "DATABASE_PORT",
    "DATABASE_USER",
    "DATABASE_DATABASE",
    "REDIS_URL",
]

# 5b 真正拿来比值的「身份」字段：连过去之后必须对得上的东西。
# 刻意**不含** DATABASE_HOST / REDIS_URL 的 host —— 它们按环境各写一份是对的。
IDENTITY_ENV_KEYS = ["DATABASE_USER", "DATABASE_DATABASE", "DATABASE_PORT"]
APP_ENV_SERVICES = ["analysis-service", "data-service"]

# 在部署配置里**不许出现**的 env 名 —— 每一条都是「曾经存在过的死键」。
BANNED_ENV_NAMES = {
    "POSTGRES_HOST": "零读取者：应用读 database.host → DATABASE_HOST，"
                     "postgres 容器也不需要知道自己的 host",
    "POSTGRES_PORT": "零读取者：同上",
    "REDIS_HOST": "零读取者：应用读 redis.url → REDIS_URL",
    "REDIS_PORT": "零读取者：同上",
    "REDIS_PASSWORD": "零读取者：本项目 redis 没有 --requirepass，不校验密码",
    "DB_PASSWORD": "零读取者：viper 推导出的名字是 DATABASE_PASSWORD。"
                   "曾与 DATABASE_PASSWORD 并存，默认值相同所以能用，改密码就必然对不上",
}

# postgres 容器**自己**的约定名：只允许出现在 postgres 自己的 env 里
# （compose 的 postgres 服务 / k8s 的 postgres-statefulset）。应用侧一律用
# DATABASE_*。
POSTGRES_CONTAINER_ENV = {"POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD"}


def derive_readable_env_names() -> tuple[set[str], set[str]]:
    """从 Go 源码推导「应用能读到的 env 名」。

    只看**非测试**文件：测试读某个键不代表生产读它。返回 (精确名, 前缀)。

    覆盖的读取形态：`Get*("a.b")` / `BindEnv("a.b")` / `os.Getenv("A")` →
    精确名；`UnmarshalKey("a")` / `Sub("a")` → 前缀 `A_`（整棵子树）；
    `ConfigKey* = "a.b"` 这类常量定义 → 精确名。

    **不覆盖** `viper.Unmarshal(&整个结构体)`（cmd/strategy 用它读 server.*）——
    它读的键只能靠结构体 tag 反推，代价不划算。这类键若进了 configmap 会被
    误报，加进 ENV_NAME_ALLOWLIST 并写明理由即可（宁可误报，不可漏报）。
    """
    names: set[str] = set()
    prefixes: set[str] = set()
    for path in sorted(ROOT.rglob("*.go")):
        if path.name.endswith("_test.go") or ".workbuddy-ai" in path.parts:
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        for m in _GO_KEY_CALL.finditer(text):
            names.add(m.group(1).upper().replace(".", "_"))
        for m in _GO_CONFIG_KEY_CONST.finditer(text):
            names.add(m.group(1).upper().replace(".", "_"))
        for regex in (_GO_UNMARSHAL_KEY_CALL, _GO_SUB_CALL):
            for m in regex.finditer(text):
                prefixes.add(m.group(1).upper().replace(".", "_") + "_")
        for m in _GO_GETENV_CALL.finditer(text):
            names.add(m.group(1))
    return names, prefixes


# 不来自上面的推导、但确实会被读到的 env 名。**加条目必须写理由** ——
# 空集合本身就是一条信息：现在没有任何例外。
ENV_NAME_ALLOWLIST: dict[str, str] = {}


def is_readable(name: str, names: set[str], prefixes: set[str]) -> bool:
    if name in names or name in ENV_NAME_ALLOWLIST:
        return True
    return any(name.startswith(p) for p in prefixes)


def parse_compose_environment(path: Path) -> dict[str, dict[str, str]]:
    """抓 docker-compose 每个 service 的 environment: 键值（原样，不展开插值）。"""
    envs: dict[str, dict[str, str]] = {}
    current: str | None = None
    in_env = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        m = re.match(r"^ {2}([A-Za-z0-9_.-]+):\s*$", line)
        if m:
            current, in_env = m.group(1), False
            continue
        if re.match(r"^ {4}environment:\s*$", line):
            in_env = True
            continue
        if not in_env:
            continue
        m = re.match(r"^ {6}([A-Z][A-Z0-9_]*):\s*(.*)$", line)
        if m:
            envs.setdefault(current, {})[m.group(1)] = m.group(2).strip().strip('"')
        elif not line.startswith("      "):
            in_env = False
    return envs


def parse_configmap_keys(text: str) -> dict[str, str]:
    """抓 configmap `data:` 下 2 空格缩进的 UPPER_CASE 键。"""
    return {
        m.group(1): m.group(2).strip().strip('"')
        for m in re.finditer(r"^ {2}([A-Z][A-Z0-9_]*):\s*(.*)$", text, re.M)
    }


def parse_deployment_env_names(path: Path) -> set[str]:
    """抓 deployment 里 `- name: UPPER_CASE` 的 env 名。

    containerPort / container 的 `- name: http` 是小写，不会误命中。
    """
    return set(
        re.findall(r"^\s*- name: ([A-Z][A-Z0-9_]*)\s*$",
                   path.read_text(encoding="utf-8"), re.M)
    )


def parse_configmap_key_refs(path: Path) -> set[str]:
    """抓 `configMapKeyRef:` 后面 `key: X` 引用的 configmap 键。"""
    keys: set[str] = set()
    pending = False
    for line in path.read_text(encoding="utf-8").splitlines():
        if "configMapKeyRef:" in line:
            pending = True
            continue
        if not pending:
            continue
        m = re.match(r"^\s*key:\s*(\S+)\s*$", line)
        if m:
            keys.add(m.group(1))
            pending = False
    return keys


def check_env_wiring(
    errors: list[str],
    compose_env: dict[str, dict[str, str]],
    k8s_config_text: str,
) -> None:
    """AUD-39 的检查 5~8：env 口径一致、可达、无占位符、死键不许回来。"""
    cm_keys = parse_configmap_keys(k8s_config_text)

    # 5a) 应用侧必备键
    for svc in APP_ENV_SERVICES:
        env = compose_env.get(svc, {})
        for key in APP_DB_ENV_KEYS + ["DATABASE_PASSWORD"]:
            if key not in env:
                errors.append(
                    f"docker-compose.yml 的 {svc} 缺 {key} —— 应用读的是这个 env 名（AUD-39）")
    for key in APP_DB_ENV_KEYS:
        if key not in cm_keys:
            errors.append(
                f"deploy/k8s/configmap.yaml 缺 {key} —— 应用读的是这个 env 名；"
                f"缺了它会静默回落到镜像内的 config/*.yaml（AUD-39）")

    # 5b) compose ↔ k8s 的**身份**字段必须相等；**地址不比**（2026-09-25 放宽）
    #
    # 数据库移到宿主机后：容器内必然是 host.docker.internal，k8s 里是 Service 名
    # postgres —— 两者必然不同。继续比 DATABASE_HOST / REDIS_URL 的 host 只会逼出
    # 一个恒假的断言，那比没有断言更糟（要么被无视，要么被「修」成错的）。
    # 保留的是「连过去之后必须对得上」的那部分：库名、用户名、端口。
    #
    # ⚠️ 这里的**逐服务循环**是破坏验证逼出来的（2026-09-25）：原实现只拿
    # `compose_env.get("analysis-service")` 去对 k8s，于是 **data-service 的
    # 库名/用户名漂移完全没人查** —— 把它改成 somebody 后检查器照旧全绿。
    # 护栏只守一半比不守更容易让人放心，所以改成对每个应用服务都比。
    for svc in APP_ENV_SERVICES:
        env = compose_env.get(svc, {})
        for key in IDENTITY_ENV_KEYS:
            c = env.get(key)
            k = cm_keys.get(key)
            if c is not None and k is not None and c != k:
                errors.append(
                    f"{svc} 的 {key} 不一致 —— compose {c} vs k8s {k}（AUD-39）")

    # 5b-2) REDIS_URL 只比**端口**，不比 host —— host 各自不同是预期的。
    for svc in APP_ENV_SERVICES:
        c_redis = compose_env.get(svc, {}).get("REDIS_URL", "")
        k_redis = cm_keys.get("REDIS_URL", "")
        c_port = re.search(r":(\d+)\s*$", c_redis)
        k_port = re.search(r":(\d+)\s*$", k_redis)
        if c_port and k_port and c_port.group(1) != k_port.group(1):
            errors.append(
                f"{svc} 的 REDIS_URL 端口不一致 —— compose {c_port.group(1)} vs "
                f"k8s {k_port.group(1)}（host 部分各自不同是预期的，不参与比对）（AUD-39）")

    # 5c) DB 身份不能与 config/*.yaml 漂移（库名/用户名对不上 = 连不到库）
    #
    # ⚠️ 同 5b：原实现循环 `name` 遍历两个服务、却固定拿
    # `compose_env.get("analysis-service")` 当对比值 —— 于是 data-service
    # 的 yaml 永远在跟 **analysis-service 的 env** 比，而不是跟它自己的。
    # 现在各比各的。破坏验证（2026-09-25）验证过改坏能被抓到。
    for name in APP_ENV_SERVICES:
        text = (ROOT / "config" / f"{name}.yaml").read_text(encoding="utf-8")
        for yaml_key, env_key in (("user", "DATABASE_USER"),
                                  ("database", "DATABASE_DATABASE")):
            value = find_nested_value(text, "database", yaml_key)
            want = compose_env.get(name, {}).get(env_key)
            if value and want and value != want:
                errors.append(
                    f"config/{name}.yaml 的 database.{yaml_key}={value} 与它自己的部署 env "
                    f"{env_key}={want} 不一致 —— 应用会去连不存在的库/用户（AUD-39）")

    # 5d) 密码只能有**一个**变量（2026-09-25 改写）。
    #
    # 原文守的是「compose 的 postgres 服务与应用侧读同一个 ${DATABASE_PASSWORD}」。
    # postgres 服务已移出 compose，那句话失去了对象 —— 不断言就等于静默丢了一条
    # 护栏。新形态下照着写：**应用侧**（compose 的两个服务）都必须从
    # DATABASE_PASSWORD 取密码，而不是内联字面量。
    #
    # k8s 侧不在此列：configmap 里本来就没有密码（它在 Secret 里），由
    # postgres-statefulset 与两个 deployment 各自 configMapKeyRef/secretKeyRef 取。
    for svc in ("analysis-service", "data-service"):
        value = compose_env.get(svc, {}).get("DATABASE_PASSWORD", "")
        if "DATABASE_PASSWORD" not in value:
            errors.append(
                f"docker-compose.yml 的 {svc} 的 DATABASE_PASSWORD 取的是 "
                f"{value or '(缺失)'} —— 必须引用同一个变量 DATABASE_PASSWORD，"
                f"否则改密码时各处不一致（AUD-39）")

    # 6) env 可达性：注入的每个 env 名都要有读取点
    names, prefixes = derive_readable_env_names()
    for key in sorted(cm_keys):
        if not is_readable(key, names, prefixes):
            errors.append(
                f"deploy/k8s/configmap.yaml 的 {key} 全仓没有任何读取点 —— "
                f"要么接上读取方，要么删掉（AUD-39）")
    for name in ("analysis-deployment.yaml", "data-deployment.yaml"):
        for env_name in sorted(parse_deployment_env_names(K8S_DIR / name)):
            if not is_readable(env_name, names, prefixes):
                errors.append(
                    f"deploy/k8s/{name} 注入的 {env_name} 全仓没有任何读取点（AUD-39）")

    # 6b) configMapKeyRef 不能悬空
    for f in sorted(K8S_DIR.glob("*.yaml")):
        for ref in sorted(parse_configmap_key_refs(f)):
            if ref not in cm_keys:
                errors.append(
                    f"deploy/k8s/{f.name} 引用了 configmap 键 {ref}，但 "
                    f"deploy/k8s/configmap.yaml 里没有这个键（AUD-39）")

    # 7) config/*.yaml 不得出现 ${...}
    #
    # 只看 `#` 之前的部分：注释里为了说明这件事，本身就要写出 `${...}` 这个
    # 形状（否则没法解释为什么它是陷阱）—— 那是引文，不是配置。
    for f in sorted((ROOT / "config").glob("*.yaml")):
        for lineno, line in enumerate(f.read_text(encoding="utf-8").splitlines(), 1):
            code = line.split("#", 1)[0]
            if "${" in code:
                errors.append(
                    f"config/{f.name}:{lineno} 出现 ${{...}} —— 本仓没有 env 展开器"
                    f"（无 os.ExpandEnv / envsubst），它不是模板，是字面量（AUD-39）")

    # 8) 负向钉：死键不许回来
    # (标签, env 名 → 值, 是否 postgres 容器自身的 env)
    surfaces: list[tuple[str, dict[str, str], bool]] = []
    for svc, env in sorted(compose_env.items()):
        surfaces.append((f"docker-compose.yml 的 {svc} 服务", env, svc == "postgres"))
    surfaces.append(("deploy/k8s/configmap.yaml", cm_keys, False))
    for f in sorted(K8S_DIR.glob("*.yaml")):
        surfaces.append((
            f"deploy/k8s/{f.name}",
            {n: "" for n in parse_deployment_env_names(f)},
            f.name == "postgres-statefulset.yaml",
        ))
    for where, env, is_pg_container in surfaces:
        for banned, why in sorted(BANNED_ENV_NAMES.items()):
            if banned in env:
                errors.append(f"{where} 出现 {banned} —— {why}（AUD-39）")
        if is_pg_container:
            continue
        for name in sorted(POSTGRES_CONTAINER_ENV):
            if name in env:
                errors.append(
                    f"{where} 出现 {name} —— 那是 postgres 容器自己的约定名，"
                    f"应用侧一律用 DATABASE_*（AUD-39）")


# ── AUD-48：Dockerfile 的 COPY 源路径必须存在 ──────────────────────────
#
# 为什么需要这一项：**编译期没有任何东西检查 Dockerfile 里 COPY 的源路径。**
# `go build` / `go vet` / `gofmt` / `check_doc_links.py` 都不读 Dockerfile，
# 而 CI 又**不构建镜像** —— 于是「删掉一个目录但忘了删引用它的 COPY」会长期
# 静默，症状不是「构建失败」，而是**那个服务从来没起来过，而且看起来没人动过它**。
#
# 已两次同型（第三次就得有护栏）：
#   1. ODR-021 删 `cmd/risk` `cmd/execution` → Dockerfile.service 的构建行没跟着改
#      （该处注释里记着「从服务合并那天起就没能构建成功过」）
#   2. AUD-33 删 `cmd/analysis/static/` → 两处 `COPY cmd/analysis/static` 没跟着删，
#      2026-09-22 完整部署时炸 `failed to calculate checksum: not found`
#
# 边界（写在这里，免得下次误报时不知道该改哪里）：
#   - **只检查不带 `--from` 的 COPY**。带 `--from` 的源路径是**另一个构建阶段内部**
#     的路径（镜像内的 /app/...），不是 build context 里的路径，静态检查不了。
#   - build context 假定为**仓库根**（compose 的 `context: .` 与 Makefile 的
#     `docker build -f cmd/analysis/Dockerfile .` 都是根）。若将来某个 Dockerfile
#     改用别的 context，这一项会误报 —— 届时给它单独记 context。
#   - 支持通配符（`COPY go.mod go.sum* ./`）与多源（`COPY a b dst`）。
#   - 含 `${...}` 的源路径跳过：本仓没有 env 展开器，但那属于另一项检查的领地。

def iter_dockerfiles() -> list[Path]:
    files = sorted(ROOT.glob("Dockerfile*"))
    files += sorted(ROOT.glob("cmd/*/Dockerfile"))
    return [p for p in files if p.is_file()]


def check_dockerfile_copy_sources(errors: list[str]) -> None:
    for df in iter_dockerfiles():
        rel = df.relative_to(ROOT).as_posix()
        for lineno, raw in enumerate(
                df.read_text(encoding="utf-8").splitlines(), start=1):
            line = raw.strip()
            if not line.startswith("COPY"):
                continue
            tokens = line.split()[1:]
            # flags：--from=builder / --chown=... 一律跳过（--from 的源不在 context 里）
            while tokens and tokens[0].startswith("--"):
                if tokens[0].startswith("--from"):
                    tokens = []
                    break
                tokens = tokens[1:]
            if len(tokens) < 2:
                continue
            for src in tokens[:-1]:
                if "$" in src or src.startswith("/"):
                    continue
                if src in {".", "..", "./", "../"}:
                    # `COPY . .` —— 整个 context，必然存在（且 pathlib.glob 不接受 '.'）
                    continue
                if any(ch in src for ch in "*?["):
                    matches = sorted(ROOT.glob(src))
                else:
                    matches = [ROOT / src] if (ROOT / src).exists() else []
                if not matches:
                    errors.append(
                        f"{rel}:{lineno} 的 `COPY {src}` 源路径不存在 —— "
                        f"删目录时要连引用它的 COPY 一起删；docker build 只在最后一步才炸，"
                        f"而 CI 不构建镜像，所以会静默很久（AUD-48）")


# ── AUD-13（2026-09-25 改写）：基础设施必须真在仓外，且容器显式指向它 ──────
#
# 这一类失败全是**静默**的：compose 语法合法、容器也能起来，只在第一次连库时
# 失败。而「Docker Desktop 不稳定 → 把数据库挪到宿主机」这个决定本身，如果
# compose 里还留着 postgres / redis 服务，就会变成两个进程抢同一个端口 ——
# 表现为「有时连得上、有时连错库」，比干脆连不上更难查。

def check_infra_is_external(
    errors: list[str],
    compose_env: dict[str, dict[str, str]],
    compose_bindings: dict[str, list[tuple[str | None, int]]],
) -> None:
    """检查 3：数据库/缓存不得由 compose 托管，且应用容器必须指向宿主机。"""
    # 3a) compose 里不得再有 postgres / redis 服务
    for svc in NATIVE_DB_DEPS:
        if svc in compose_env or svc in compose_bindings:
            errors.append(
                f"docker-compose.yml 仍有 {svc} 服务 —— 数据库/缓存已改为宿主机原生"
                f"安装（见文件头），容器版会与原生进程抢同一个端口（AUD-13）")

    # 3b) 应用服务必须显式注入宿主机地址，且值必须指向宿主机
    for svc, required in sorted(INFRA_ENV_REQUIRED.items()):
        env = compose_env.get(svc, {})
        for key, kind in required:
            value = env.get(key)
            if value is None:
                errors.append(
                    f"docker-compose.yml 的 {svc} 缺 {key} —— config/*.yaml 现在存的是"
                    f"宿主机视角（localhost），漏注入会让容器回落到 localhost 并连到"
                    f"容器自己（AUD-13）")
                continue
            if kind == "host" and value != CONTAINER_HOST_ALIAS:
                errors.append(
                    f"docker-compose.yml 的 {svc} 的 {key}={value} —— 容器内必须指向 "
                    f"{CONTAINER_HOST_ALIAS}（容器里的 localhost 是容器自己）（AUD-13）")
            if kind == "url" and CONTAINER_HOST_ALIAS not in value:
                errors.append(
                    f"docker-compose.yml 的 {svc} 的 {key}={value} —— URL 里必须出现 "
                    f"{CONTAINER_HOST_ALIAS}（容器里的 localhost 是容器自己）（AUD-13）")



def main() -> int:
    if not COMPOSE.exists():
        print(f"✗ 找不到 {COMPOSE}")
        return 1

    compose_text = COMPOSE.read_text(encoding="utf-8")
    compose_bindings = parse_compose_ports(COMPOSE)
    # 一致性比对只关心宿主端口（每个服务的第一个映射），绑定地址由检查 3 管。
    compose_svc = {
        svc: ports[0][1] for svc, ports in compose_bindings.items() if ports
    }
    k8s_svc = parse_k8s_ports()

    errors: list[str] = []
    notes: list[str] = []

    # 1) 两边都有的服务，端口必须一致
    for name, cport in sorted(compose_svc.items()):
        if name in IGNORED_SERVICES:
            continue
        kport = k8s_svc.get(name)
        if kport is None:
            reason = ALLOWED_MISSING_IN_K8S.get(name)
            if reason:
                notes.append(f"{name}: k8s 未部署（{reason}）")
            else:
                errors.append(
                    f"{name}: compose 里有（:{cport}）但 k8s 里没有对应 Service "
                    f"—— 是真漂移还是有意不部署？后者请加进 ALLOWED_MISSING_IN_K8S"
                )
            continue
        if kport != cport:
            errors.append(f"{name}: 端口不一致 —— compose {cport} vs k8s {kport}")

    # 2) DATA_SERVICE_URL 两边必须一致，且指向的端口要对得上 data-service
    k8s_config_text = (K8S_DIR / "configmap.yaml").read_text(encoding="utf-8")
    compose_url = find_env_value(compose_text, "DATA_SERVICE_URL")
    k8s_url = find_env_value(k8s_config_text, "DATA_SERVICE_URL")

    if compose_url is None:
        errors.append("docker-compose.yml 缺 DATA_SERVICE_URL")
    if k8s_url is None:
        errors.append("deploy/k8s/configmap.yaml 缺 DATA_SERVICE_URL —— 此前正是缺这一项，"
                      "全靠代码默认值碰巧能用（P1-8）")
    if compose_url and k8s_url and compose_url != k8s_url:
        errors.append(f"DATA_SERVICE_URL 不一致 —— compose {compose_url} vs k8s {k8s_url}")

    for where, url in (("compose", compose_url), ("k8s", k8s_url)):
        if not url:
            continue
        m = re.match(r"^https?://([^:/]+):(\d+)", url)
        if not m:
            errors.append(f"{where} 的 DATA_SERVICE_URL 格式不对：{url}")
            continue
        host, port = m.group(1), int(m.group(2))
        actual = compose_svc.get(host) if where == "compose" else k8s_svc.get(host)
        if actual is None:
            errors.append(f"{where}: DATA_SERVICE_URL 指向 {host}，但找不到这个服务")
        elif actual != port:
            errors.append(
                f"{where}: DATA_SERVICE_URL 指向 {host}:{port}，而 {host} 实际端口是 {actual}")

    # 3) 数据库 / 缓存必须真在仓外，且应用容器显式指向宿主机（AUD-13 改写）
    compose_env = parse_compose_environment(COMPOSE)
    check_infra_is_external(errors, compose_env, compose_bindings)

    # 4) gin 的运行模式只能有一个来源：config/*.yaml 的 server.gin_mode（AUD-29）
    #
    # gin 自己在 init() 里读 GIN_MODE 环境变量（gin/mode.go:52），所以在部署配置里
    # 写 GIN_MODE 等于给同一个决定开第二个入口 —— 而且是**看不见**的那个：全仓
    # grep 不到任何读取点，只有 gin 的内部实现知道它存在。此前
    # deploy/k8s/configmap.yaml 就写着 GIN_MODE: "release"，它确实生效，但只在
    # k8s 生效（compose 没写）—— 两条部署路径的 gin mode 来源不同却不报错。
    for where, text in (("docker-compose.yml", compose_text),
                        ("deploy/k8s/configmap.yaml", k8s_config_text)):
        if find_env_value(text, "GIN_MODE") is not None:
            errors.append(
                f"{where} 设置了 GIN_MODE —— gin mode 的唯一来源是 config/*.yaml 的 "
                f"server.gin_mode（AUD-29）；gin 会自己读 GIN_MODE，写在这里会绕过它")

    # 5~8) env 口径（AUD-39）
    check_env_wiring(errors, compose_env, k8s_config_text)

    # 9) Dockerfile 的 COPY 源路径必须存在（AUD-48）
    check_dockerfile_copy_sources(errors)

    # 输出
    for n in notes:
        print(f"· {n}")
    if errors:
        print(f"\n✗ 部署配置漂移 {len(errors)} 处：")
        for e in errors:
            print(f"  - {e}")
        return 1

    print("✓ 部署配置一致（compose ↔ k8s：服务端口 + DATA_SERVICE_URL）")
    print("✓ 数据库/缓存已移出 compose，应用容器显式指向 host.docker.internal（AUD-13 改写）")
    print("✓ gin mode 无 GIN_MODE 旁路（唯一来源 server.gin_mode）")
    print("✓ env 口径一致（compose ↔ k8s 的库名/用户名/端口相同 —— 地址按环境各写"
          "一份，刻意不比；且与 config/*.yaml 的库名/用户名一致）")
    print("✓ 注入的每个 env 都有读取点（configmap 键 + 应用 deployment 的 env）")
    print("✓ config/*.yaml 无 ${...} 占位符（本仓没有 env 展开器）")
    print("✓ 死键负向钉（POSTGRES_HOST/PORT、REDIS_HOST/PORT、"
          "REDIS_PASSWORD、DB_PASSWORD 均未出现）")
    print("✓ Dockerfile 的 COPY 源路径都存在（删目录时别落下引用它的 COPY）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
