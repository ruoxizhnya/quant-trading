// Package bootstrap 收拢各服务的共享装配 builder（AI 拆仓 OBS-12 后续 /
// ADR-027 §5 第 8 步的前置切片）。
//
// 为什么存在：cmd/analysis 的 setup.go 本就是模块化 builder 集合（S7-P2-3），
// 新增 cmd/ai（ai-service）需要其中大部分 builder。复制 = 两份真相
// （OBS-06/08 同类病根），故上收为共享包。cmd/analysis 保留同名薄委托
// shim，装配调用点与测试**零改动**。
//
// 阶段 2（pkg/ai 搬入 quant-trading-agent 仓）时，AI 相关 builder
// （BuildCopilot / BuildToolsRegistry / 各 adapter）随 pkg/ai 迁走，
// 本包只留非 AI 的服务骨架。
package bootstrap

import (
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/observability"
)

// HTTPClient 是各服务共享的出站 HTTP client：统一超时 + 可观测 transport
// （Sprint 6 P0-3：传播 X-Request-ID、记录 http_client_requests_total）。
//
// Service 标签沿用 analysis 时代的 "data" 取值 —— outbound 指标按
// {service="data"} 聚合的既有口径不变（改口径会让历史面板断线）。
var HTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &observability.HTTPTransport{
		Service: "data",
	},
}

// InitLogger 构造带时间戳的标准 zerolog logger。
func InitLogger() zerolog.Logger {
	return zerolog.New(os.Stdout).With().Timestamp().Logger()
}

// InitMetrics 构造四条 ADR-017 §1 核心指标，注册进 Prometheus 默认
// registry，附加 Go runtime collectors，并把指标接进 HTTPClient 的
// transport，使出站调用记录 http_client_requests_total。
func InitMetrics(logger zerolog.Logger) *observability.Metrics {
	m := observability.NewMetrics()
	m.Register()
	m.RegisterCollectors(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	if t, ok := HTTPClient.Transport.(*observability.HTTPTransport); ok {
		t.Metrics = m
	}
	logger.Info().Msg("observability: 4 core metrics registered (ADR-017 §1)")
	return m
}

// RequestLogger 返回逐请求摘要日志中间件（method/path/status/latency）。
func RequestLogger(logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info().
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Dur("latency", time.Since(start)).
			Msg("request")
	}
}
