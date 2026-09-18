#!/usr/bin/env python3
"""校验 docker-compose.yml 与 deploy/k8s/*.yaml 的关键项是否一致（P1-8）。

背景：本地开发走 docker-compose，部署走 k8s，两套配置独立维护 —— 改了一边
忘了另一边就会漂移，而且漂移往往是**静默**的：服务起得来，只在某个调用
跨服务时才失败（比如 analysis 找不到 data-service）。

本脚本只比对**会互相影响**的关键项：
  1. 服务名 → 端口（公共服务必须一致）
  2. DATA_SERVICE_URL：compose 的 environment 与 k8s configmap 必须一致，
     且它指向的 host:port 要真的等于 data-service 的端口

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


def parse_compose(path: Path) -> dict[str, int]:
    """抓 docker-compose 的 service → 宿主端口（取第一处 "A:B" 的 A）。"""
    services: dict[str, int] = {}
    current: str | None = None
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        m = re.match(r"^ {2}([A-Za-z0-9_.-]+):\s*$", line)
        if m:
            current = m.group(1)
            continue
        m = re.match(r'^\s*-\s*"?(\d+):(\d+)"?\s*$', line)
        if m and current and current not in services:
            services[current] = int(m.group(1))
    return services


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
    compose_svc = parse_compose(COMPOSE)
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
    compose_url = find_env_value(compose_text, "DATA_SERVICE_URL")
    k8s_url = find_env_value((K8S_DIR / "configmap.yaml").read_text(encoding="utf-8"),
                             "DATA_SERVICE_URL")

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

    # 输出
    for n in notes:
        print(f"· {n}")
    if errors:
        print(f"\n✗ 部署配置漂移 {len(errors)} 处：")
        for e in errors:
            print(f"  - {e}")
        return 1

    print("✓ 部署配置一致（compose ↔ k8s：服务端口 + DATA_SERVICE_URL）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
