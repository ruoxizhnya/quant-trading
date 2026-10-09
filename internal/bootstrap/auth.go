package bootstrap

import (
	"fmt"
	"net"
	"strings"

	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/ruoxizhnya/quant-trading/pkg/storage"
	"github.com/spf13/viper"
)

// InsecureExposureLoopbackPublished 是 auth.allow_insecure_exposure
// （env: AUTH_INSECURE_EXPOSURE）唯一承认的取值。
//
// 它存在的理由：`server.host` 描述的是**进程绑定的网卡**，不是**这个实例
// 真实的对外可达性**。在容器里这两者会脱钩 —— 容器必须绑 0.0.0.0 才能让
// 发布端口把流量转进来，但「谁能连上」完全由 `ports:` 的映射决定。
// 于是「容器内绑定 0.0.0.0 + 只发布到 127.0.0.1」这一组合，对外可达性
// 等价于 loopback，却会被原来的 isLoopbackHost 判成「暴露到局域网」而拒绝。
//
// 所以这里**不是删掉那道门，而是换一个判据**：operator 明确声明
// 「本实例的对外可达性由发布层限定为 loopback」。声明本身不做安全保证 ——
// 保证由两处机器校验给出，声明只是让「有人主动承诺过」这件事在配置里可见：
//
//  1. 静态：tools/check_deploy_consistency.py 的检查 3c —— 只要
//     AUTH_INSECURE 为真，**每一个** app 服务的**每一条** ports 映射都必须带
//     127.0.0.1: 前缀；反之只要有非回环映射，就必须给 JWT_SECRET。
//  2. 运行时：tools/local-stack.sh 读宿主机真实 netstat，断言 8080/8081/
//     8082/8085/8086 只监听回环（与 AUD-13 对 PG/Redis 的做法同源）。
//
// 取值写错（拼错、空、写一半）一律按「没声明」处理 → 拒绝启动，fail closed。
const InsecureExposureLoopbackPublished = "loopback-published"

// AuthExposureOK 报告 exposure 声明是否被承认。
func AuthExposureOK(exposure string) bool {
	return strings.TrimSpace(exposure) == InsecureExposureLoopbackPublished
}

// AuthStartup 是启动期对鉴权配置的裁决结果。做成纯值是为了可测 ——
// 真正的 os.Exit 只在 InitAuth 里发生一次，测试测 DecideAuthStartup 即可。
type AuthStartup struct {
	Secret   []byte // 空 = open-access
	Insecure bool   // 明确处于「无鉴权」模式
	Refuse   bool   // 拒绝启动
	Reason   string // 拒绝原因（必须给出可操作的修复指引）
}

// DecideAuthStartup 裁决启动期的鉴权配置（P0-4）。
//
// 契约：**没有密钥就拒绝启动**。此前密钥为空会静默进入 open-access —
// 任何能访问网络的人都能触发回测、创建订单。修成 fail-closed 后，
// 唯一豁免是「显式声明不安全的本地模式」，且必须是下面二者之一：
//
//	A. auth.allow_insecure=true（env: AUTH_INSECURE）且 server.host 是 loopback
//	B. auth.allow_insecure=true 且 auth.insecure_exposure=loopback-published
//	   —— 用于容器：绑定是 0.0.0.0，但发布层限定为 127.0.0.1
//	   （见 InsecureExposureLoopbackPublished 的说明与两处机器校验）
//
// 非 loopback 监听且没有 B 的声明时照样拒绝 —— 0.0.0.0 上的 open-access
// 等于把下单接口开给整个局域网。
func DecideAuthStartup(secret string, allowInsecure bool, bindHost string, insecureExposure string) AuthStartup {
	if s := strings.TrimSpace(secret); s != "" {
		return AuthStartup{Secret: []byte(s)}
	}
	if !allowInsecure {
		return AuthStartup{
			Refuse: true,
			Reason: "auth: JWT secret missing — refusing to start in open-access mode. " +
				"Set JWT_SECRET (or auth.jwt_secret) to a strong random value. " +
				"Local dev only: set AUTH_INSECURE=true AND server.host=127.0.0.1",
		}
	}
	if IsLoopbackHost(bindHost) {
		return AuthStartup{Insecure: true}
	}
	if AuthExposureOK(insecureExposure) {
		// 容器形态：绑定 0.0.0.0，对外可达性由发布层限定。
		// 这里只做「声明是否合法」，真实暴露面由静态护栏 + 运行时 netstat 断言保证。
		return AuthStartup{Insecure: true}
	}
	return AuthStartup{
		Refuse: true,
		Reason: fmt.Sprintf("auth: AUTH_INSECURE=true but server.host=%q is not loopback — "+
			"open-access would be reachable from other hosts. "+
			"Set server.host=127.0.0.1, or configure JWT_SECRET instead. "+
			"Containers (which must bind 0.0.0.0) must instead publish on loopback and declare "+
			"AUTH_INSECURE_EXPOSURE=%s", bindHost, InsecureExposureLoopbackPublished),
	}
}

// IsLoopbackHost 报告 host 是否只监听本机。空 host 视为非 loopback —
// gin 绑 ":port" 等价于 0.0.0.0，fail closed 更安全。
func IsLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// InitAuth 构造 JWT + RBAC auth service（P1-2, ADR-017 §2）并执行 P0-4
// 启动门：没有密钥就拒绝启动（除非 operator 显式选择 loopback-only
// open access）。
//
// 密钥来源（按优先级）：JWT_SECRET env → AUTH_JWT_SECRET env →
// auth.jwt_secret 配置项。YAML 是入库的，生产一律走 env。
func InitAuth(v *viper.Viper, store *storage.PostgresStore, logger zerolog.Logger) *auth.Service {
	d := DecideAuthStartup(
		v.GetString("auth.jwt_secret"),
		v.GetBool("auth.allow_insecure"),
		v.GetString("server.host"),
		v.GetString("auth.insecure_exposure"),
	)
	if d.Refuse {
		logger.Fatal().Msg(d.Reason)
	}

	authSvc := auth.NewService(store.DB(), auth.Config{
		JWTSecret:       d.Secret,
		AccessTokenTTL:  v.GetDuration("auth.access_token_ttl"),
		RefreshTokenTTL: v.GetDuration("auth.refresh_token_ttl"),
		Issuer:          v.GetString("auth.issuer"),
	})
	if authSvc.Enabled() {
		logger.Info().
			Int("access_ttl_sec", int(authSvc.AccessTTL().Seconds())).
			Msg("auth: JWT enabled (P1-2)")
	} else {
		ev := logger.Warn()
		if !IsLoopbackHost(v.GetString("server.host")) && AuthExposureOK(v.GetString("auth.insecure_exposure")) {
			// 容器形态。这条日志是给「出事之后翻日志」的人看的：它把
			// 「谁在保证暴露面」写清楚，免得误以为这条豁免是白来的。
			ev = ev.Str("exposure", InsecureExposureLoopbackPublished).
				Str("guaranteed_by", "tools/check_deploy_consistency.py (static) + tools/local-stack.sh (netstat)")
		}
		ev.Msg("auth: INSECURE open-access mode — NO authentication, loopback only, do not use outside local dev")
	}
	return authSvc
}
