#!/usr/bin/env python3
"""校验 docker-compose.yml 与 deploy/k8s/*.yaml 的关键项是否一致（P1-8）。

背景：本地开发走 docker-compose，部署走 k8s，两套配置独立维护 —— 改了一边
忘了另一边就会漂移，而且漂移往往是**静默**的：服务起得来，只在某个调用
跨服务时才失败（比如 analysis 找不到 data-service）。

本脚本只比对**会互相影响**的关键项：
  1. 服务名 → 端口（公共服务必须一致）
  2. DATA_SERVICE_URL：compose 的 environment 与 k8s configmap 必须一致，
     且它指向的 host:port 要真的等于 data-service 的端口
  3. postgres / redis 的端口必须**只绑回环**（AUD-13）。这条不是「两边一致」
     而是「单边不该有的暴露」，但它同属部署配置的护栏，放这里比另起脚本划算。
  4. 部署配置里不得出现 GIN_MODE（AUD-29）。gin mode 的唯一来源是
     config/*.yaml 的 server.gin_mode；GIN_MODE 会被 gin 自己读走，是同一个
     决定的第二个入口，而且 grep 不到读取点。

刻意不做的事：
  - 不做「一份源生成两份」。那要引入 Kompose / Helm 之类的工具，为了
    几处配置给单人自托管项目加一整套生成链，收益不抵复杂度。
  - 不要求服务集合完全相同。k8s 里没部署 strategy-service（standby per
    ADR-012）是**有意的**，写死在 ALLOWED_K8S_ONLY_MISSING 里。

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
ALLOWED_MISSING_IN_K8S = {
    "strategy-service": "standby per ADR-012，k8s 里不部署",
    "postgres": "k8s 用 postgres-statefulset.yaml，不走 Deployment/Service",
    "redis": "k8s 用 redis-deployment.yaml",
}

# 两边都可能不配的最小集合（比如只在本地跑的辅助容器）
IGNORED_SERVICES = {"equitydeep-research"}

# AUD-13：只该绑回环的服务 —— 数据库与缓存不该出现在局域网上。
# 应用服务（data / strategy / analysis）**有意不在**此列：它们本来就是要被
# 访问的（analysis 另有 JWT fail-closed 兜底）。
LOOPBACK_ONLY = {"postgres", "redis"}

# 可接受的「回环」写法。写成 0.0.0.0 / 具体网卡 IP / 干脆不写宿主地址，
# 都算暴露到局域网。
LOOPBACK_BINDS = {"127.0.0.1", "::1", "localhost"}


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

    # 3) 数据库 / 缓存必须只绑回环（AUD-13）
    for svc in sorted(LOOPBACK_ONLY):
        for bind, port in compose_bindings.get(svc, []):
            if bind is None:
                errors.append(
                    f"{svc}:{port} 绑在 0.0.0.0（端口映射没写宿主地址）—— "
                    f"数据库/缓存会暴露到局域网，改成 \"127.0.0.1:{port}:{port}\"")
            elif bind not in LOOPBACK_BINDS:
                errors.append(
                    f"{svc}:{port} 绑在 {bind} —— 只允许回环地址 "
                    f"（{', '.join(sorted(LOOPBACK_BINDS))}）")

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

    # 输出
    for n in notes:
        print(f"· {n}")
    if errors:
        print(f"\n✗ 部署配置漂移 {len(errors)} 处：")
        for e in errors:
            print(f"  - {e}")
        return 1

    print("✓ 部署配置一致（compose ↔ k8s：服务端口 + DATA_SERVICE_URL）")
    print("✓ 数据库/缓存只绑回环（postgres / redis）")
    print("✓ gin mode 无 GIN_MODE 旁路（唯一来源 server.gin_mode）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
