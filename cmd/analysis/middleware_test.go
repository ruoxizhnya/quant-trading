package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P0-4: 本文件只守「装配」——中间件实现本身的契约在
// internal/httpserver/cors_test.go。装配是很容易被漏掉的一环：
// 实现改对了但 buildRouter 没接上，等于没改。

// writeTempConfig 写一份最小可用配置到临时目录，返回路径。
// loadConfig 读不到配置文件会 Fatal，所以必须落盘真实文件。
func writeTempConfig(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "cfg.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0644))
	return p
}

func routerWithProbe(t *testing.T, authSvc *auth.Service, yaml string) *gin.Engine {
	t.Helper()
	t.Setenv("CONFIG_PATH", writeTempConfig(t, t.TempDir(), yaml))
	v := loadConfig(zerolog.Nop())
	r := buildRouter(authSvc, v, zerolog.Nop())
	r.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestBuildRouter_WiresCORSAllowlist(t *testing.T) {
	r := routerWithProbe(t, auth.NewService(nil, auth.Config{}), `
server:
  host: 127.0.0.1
  cors:
    allowed_origins:
      - http://localhost:5173
logging:
  level: info
`)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))

	req = httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestBuildRouter_NoAllowlistFailsClosed(t *testing.T) {
	r := routerWithProbe(t, auth.NewService(nil, auth.Config{}), `
server:
  host: 127.0.0.1
logging:
  level: info
`)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"未配置白名单时不得回显任何来源")
}

func TestBuildRouter_MountsAuthOnlyWhenEnabled(t *testing.T) {

	// 无密钥：open-access（仅 loopback + 显式豁免下才允许，见 decideAuthStartup）。
	open := routerWithProbe(t, auth.NewService(nil, auth.Config{}), `
server:
  host: 127.0.0.1
logging:
  level: info
`)
	w := httptest.NewRecorder()
	open.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// 有密钥：鉴权中间件必须生效，未带 token 的请求应被挡下。
	secured := routerWithProbe(t, auth.NewService(nil, auth.Config{JWTSecret: []byte("test")}), `
server:
  host: 127.0.0.1
logging:
  level: info
`)
	w = httptest.NewRecorder()
	secured.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
