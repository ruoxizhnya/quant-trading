package httpserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P0-4: 此前 cmd/analysis 与 cmd/data 各有一份复制粘贴的 corsMiddleware，
// 都硬编码 `Access-Control-Allow-Origin: *`。收口到这里之后，下面这组测试
// 同时守护两个服务。

func newTestRouter(allowed []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(allowed))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.POST("/order", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func get(t *testing.T, r *gin.Engine, origin string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORS_AllowedOriginEchoed(t *testing.T) {
	r := newTestRouter([]string{"http://localhost:5173"})
	w := get(t, r, "http://localhost:5173")

	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Origin", w.Header().Get("Vary"))
}

func TestCORS_UnknownOriginNotEchoed(t *testing.T) {
	r := newTestRouter([]string{"http://localhost:5173"})
	w := get(t, r, "https://evil.example")

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"白名单外的来源不得回显，否则等于放行任意站点")
	assert.Equal(t, http.StatusOK, w.Code, "非预检请求照常处理，由浏览器侧拦截")
}

func TestCORS_EmptyAllowlistFailsClosed(t *testing.T) {
	// 没配白名单 = 不信任任何跨源，连 localhost 也不回显。
	r := newTestRouter(nil)
	w := get(t, r, "http://localhost:5173")

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_ExplicitWildcard(t *testing.T) {
	// "*" 必须显式写进配置才生效，不再是默认值。
	r := newTestRouter([]string{"*"})
	w := get(t, r, "https://anything.example")

	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NoOriginHeaderPassesThrough(t *testing.T) {
	// 同源请求 / curl / 服务间调用没有 Origin 头：不需要 CORS 头，也不该被拦。
	r := newTestRouter([]string{"http://localhost:5173"})
	w := get(t, r, "")

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCORS_PreflightAllowed(t *testing.T) {
	r := newTestRouter([]string{"http://localhost:5173"})

	req := httptest.NewRequest(http.MethodOptions, "/order", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "http://localhost:5173", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestCORS_PreflightRejected(t *testing.T) {
	r := newTestRouter([]string{"http://localhost:5173"})

	req := httptest.NewRequest(http.MethodOptions, "/order", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestAllowedOrigins_Parsing(t *testing.T) {
	// 同一个键要同时吃 YAML 列表、逗号分隔的 env 串、以及带空格的写法。
	cases := []struct {
		name string
		yaml string
		env  string
		want []string
	}{
		{
			name: "yaml list",
			yaml: "server:\n  cors:\n    allowed_origins:\n      - http://a.example\n      - http://b.example\n",
			want: []string{"http://a.example", "http://b.example"},
		},
		{
			name: "env comma separated",
			yaml: "logging:\n  level: info\n",
			env:  "http://a.example,http://b.example",
			want: []string{"http://a.example", "http://b.example"},
		},
		{
			name: "env with spaces",
			yaml: "logging:\n  level: info\n",
			env:  "http://a.example , http://b.example",
			want: []string{"http://a.example", "http://b.example"},
		},
		{
			name: "unset",
			yaml: "logging:\n  level: info\n",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "cfg.yaml")
			require.NoError(t, os.WriteFile(p, []byte(tc.yaml), 0644))

			v := viper.New()
			v.SetConfigFile(p)
			v.SetConfigType("yaml")
			v.AutomaticEnv()
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
			require.NoError(t, v.ReadInConfig())

			if tc.env != "" {
				t.Setenv("SERVER_CORS_ALLOWED_ORIGINS", tc.env)
			}
			assert.Equal(t, tc.want, AllowedOrigins(v))
		})
	}
}
