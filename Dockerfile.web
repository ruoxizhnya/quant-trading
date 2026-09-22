# Dockerfile.web — Vue SPA（web/）的构建与托管（AUD-32）
#
# ⚠️ 为什么必须在镜像里构建、而不是 COPY 宿主的 web/dist
#   1. `web/dist` 被 .gitignore（:56）与 .dockerignore 同时排除 ——
#      镜像构建时根本拿不到它，硬 COPY 会直接失败；
#   2. 就算能拿到也不该用：宿主那份是 Windows 下构建的，塞进 linux 镜像
#      只是埋雷（且它此刻是 2026-09-17 的旧产物，早就和源码不一致了）。
# 所以 dist 一律在 stage 1 重新构建，stage 2 只带产物 —— 最终镜像里
# 没有 node、没有源码、没有 node_modules。

# ── Stage 1: build ──────────────────────────────────────────────
FROM node:22-alpine AS web-build

WORKDIR /web

# 先只拷依赖清单再 npm ci：源码变了而依赖没变时，这一层能命中缓存。
# 反过来的写法（先 COPY web/ 再 npm ci）会让每次改一行 vue 都重跑一遍
# npm ci —— 那是整个构建里最慢的一步。
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
# `npm run build` = `vue-tsc -b && vite build`。保留类型检查是故意的：
# 类型错就构建失败，别让一个类型错误的产物被部署出去。
RUN npm run build

# ── Stage 2: serve ──────────────────────────────────────────────
FROM nginx:1.27-alpine

# 官方镜像的 default.conf 只监听 80、没有反代，整份换掉。
COPY deploy/nginx-spa.conf /etc/nginx/conf.d/default.conf

COPY --from=web-build /web/dist /usr/share/nginx/html

# nginx:alpine 里没有 curl，而 compose 的 healthcheck 要用它
# （与 Dockerfile.service 的最终镜像保持一致 —— 排障时行为一致比省几 MB 值钱）。
RUN apk add --no-cache curl

# 8080 而不是 80：80 在 nginx:alpine 里要 root 才绑得上，8080 不必。
# compose 与 k8s 两侧都用这个端口（由 tools/check_deploy_consistency.py 守护）。
EXPOSE 8080

# nginx 官方镜像自带前台运行的 CMD，不需要 ENTRYPOINT。
