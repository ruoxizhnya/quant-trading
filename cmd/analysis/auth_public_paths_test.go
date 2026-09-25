package main

// 本文件守的是「公开路由」这条缝。
//
// 鉴权中间件挂在 router 上，**先于路由匹配**运行，靠的是 pkg/auth 里那份
// 手抄的 publicPaths 名单（逐字相等匹配）。于是有一个很难查的失效形态：
// 新加一个公开端点、忘了往名单里加一行 → 这个端点永远 401，而且**只有
// 开着鉴权时才 401**（关掉鉴权时中间件是 no-op，测试全绿）。
//
// 具体到这次：/api/auth/status 与 /api/auth/bootstrap 就是 SPA 在没有凭据
// 时的唯一入口。少了哪一条，前端连"该显示登录页还是"创建首个管理员"页"
// 都问不出来 —— 页面白屏，而后端日志里只有一行 401。
//
// 所以两腿都要：
//   - AST 腿（TestAuthPublicRoutes_AreWhitelisted）：路由表与名单**逐条对齐**，
//     防的是"新加了路由没加名单"；
//   - 行为腿（TestAuthPublicRoutes_ReachableWithoutToken）：证明中间件真的在
//     看那份名单，防的是"名单对了但没人读它"。
//   - 契约腿（TestAuthStatus_…)：SPA 真正依赖的那个分支决策，走真 router +
//     真中间件 + 真库跑一遍。

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/auth"
)

// authTestRouter 只装两样东西：鉴权中间件 + /api/auth/* 路由。
//
// **故意不走 buildRouter**：buildRouter 还会挂 AuditMiddleware，而它在
// pool 为 nil 时会在 RecordAudit 里 panic（被 gin.Recovery 兜住，测试仍会
// 通过，但输出一堆吓人的栈）。那具 nil pool 是测试造出来的，不是生产形态 ——
// 生产里开着鉴权就一定连着库。本文件要测的是「公开豁免名单」，不是装配，
// 装配由 middleware_test.go 的 TestBuildRouter_MountsAuthOnlyWhenEnabled 守。
func authTestRouter(svc *auth.Service) *gin.Engine {
	r := gin.New()
	r.Use(svc.Middleware())
	registerAuthRoutes(r, svc, zerolog.Nop())
	return r
}

// publicAuthRoutesFromSource 扫 handlers_auth.go 的 registerAuthRoutes，取出
// 「公开组」（`g := router.Group("/api/auth")`）上注册的全部完整路径。
//
// 用 AST 而不是字符串包含：这个文件里到处是解释性的注释，扫原文会误报；
// 而且只有 AST 能区分「这行是注册」和「这行是注释里提了一句」。
func publicAuthRoutesFromSource(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	require.NoError(t, err, "解析 %s 失败", file)

	// pass 1：找 `X := <recv>.Group("/api/auth")`，记下接收者名字。
	groupVar := ""
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		p, err := strconv.Unquote(lit.Value)
		if err != nil || p != "/api/auth" {
			return true
		}
		id, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		groupVar = id.Name
		return false
	})
	require.NotEmpty(t, groupVar,
		"在 %s 里没找到 `X := router.Group(\"/api/auth\")` —— 扫描本身失效了", file)

	// pass 2：找 `X.<METHOD>("<path>", ...)`。
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != groupVar {
			return true
		}
		switch sel.Sel.Name {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		p, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		out = append(out, "/api/auth"+p)
		return true
	})
	return out
}

func TestAuthPublicRoutes_AreWhitelisted(t *testing.T) {
	routes := publicAuthRoutesFromSource(t, "handlers_auth.go")

	// 这一条不是形式主义：扫描若因重构（换接收者名、改用 router.Group 的其它
	// 写法）而一无所获，下面的循环就是永真 —— 那正是「假护栏」的形态。
	require.NotEmpty(t, routes,
		"AST 扫描没找到任何公开路由：先修扫描，别把永真当通过")

	whitelist := auth.PublicPaths()
	for _, r := range routes {
		assert.Contains(t, whitelist, r,
			"%s 注册在公开组上，但不在 pkg/auth 的 publicPaths 名单里 —— "+
				"中间件先于路由匹配，这一条会永远 401", r)
	}

	// 另一侧：名单里的 auth 路径必须真的有路由，否则是删了端点忘了删名单。
	routeSet := map[string]bool{}
	for _, r := range routes {
		routeSet[r] = true
	}
	for _, p := range whitelist {
		if !strings.HasPrefix(p, "/api/auth/") {
			continue // /health、/metrics 注册在 registerRoutes，不在本文件
		}
		assert.True(t, routeSet[p],
			"%s 在 publicPaths 名单里，但 handlers_auth.go 的公开组上找不到它", p)
	}
}

// TestAuthPublicRoutes_ReachableWithoutToken 是行为腿：真的发一个没有 token
// 的请求，看中间件放不放行。
//
// 探针选 POST /api/auth/bootstrap 且 body 故意不合法：它在 CreateFirstAdmin
// 之前就被 binding 挡成 400，因此**不碰数据库**，不需要真库就能跑。名单缺了
// 这条路径时，中间件会在更早处给 401 —— 两种状态 (400 / 401) 清晰可分。
func TestAuthPublicRoutes_ReachableWithoutToken(t *testing.T) {
	svc := auth.NewService(nil, auth.Config{JWTSecret: []byte("test-secret")})
	require.True(t, svc.Enabled())

	r := authTestRouter(svc)

	// 正面证据：公开端点在没有 token 时够得着（handler 回 400，而非 401）。
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"bootstrap 必须无需凭据即可到达（回 400 说明到了 handler；回 401 说明中间件名单漏了它）")

	// 反证腿：需要凭据的路由仍然必须 401。没有这一条，上面那个断言分不清
	// 「名单对了」和「整个中间件没生效」。
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/me", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"/api/auth/me 必须仍需凭据 —— 否则上面的 400 不能说明任何事")
}

// TestAuthStatus_DrivesTheSpaBranch 走真 router（真中间件）+ 真库，钉住 SPA
// 唯一的引导分支决策。
//
// 这是本次改动的产品契约：前端在**没有凭据**的情况下问
// 「要不要给你看登录页、还是"创建首个管理员"页」，答案只有这两个布尔。
// 少了 /api/auth/status 的公开豁免，这一条在开鉴权时会 401 —— 前端只能白屏。
//
// 跳过条件与断言同源：断言的前提是 users 表可读且为空，那就只在这个前提下跑，
// 并且拒绝在已有用户的库上跑（那是别人的账号，不能删）。
func TestAuthStatus_DrivesTheSpaBranch(t *testing.T) {
	pool := analysisTestPool(t)
	requireEmptyUsersForAnalysis(t, pool)

	svc := auth.NewService(pool, auth.Config{JWTSecret: []byte("test-secret")})
	r := authTestRouter(svc)

	status := func() (int, authStatusBody) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
		var body authStatusBody
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	// 空表：开鉴权 + 窗口开着 ⇒ 前端该显示「创建首个管理员」。
	code, body := status()
	require.Equal(t, http.StatusOK, code,
		"/api/auth/status 必须无需凭据即可到达；body=%s", body)
	assert.True(t, body.AuthEnabled, "开了鉴权就该这么报")
	assert.True(t, body.BootstrapRequired, "空表时窗口应当是开的")

	// 灌一行用户（不经过 HTTP）：窗口关闭 ⇒ 前端该显示登录页。
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (username, password_hash, role) VALUES ('drives-spa', 'x', 'admin')`)
	require.NoError(t, err)

	code, body = status()
	require.Equal(t, http.StatusOK, code)
	assert.True(t, body.AuthEnabled)
	assert.False(t, body.BootstrapRequired, "已有用户后窗口必须关闭")
}

// authStatusBody 只映射本测试关心的两个字段，避免把 handler 的响应结构
// 通过测试耦合到别处。
type authStatusBody struct {
	AuthEnabled       bool `json:"auth_enabled"`
	BootstrapRequired bool `json:"bootstrap_required"`
}

// analysisTestPool 连真库；连不上/表不在就 skip（`go test ./...` 必须在没有
// PostgreSQL 的机器上也是绿的）。DSN 与 pkg/storage/postgres_test.go 一致，
// 允许用 TEST_DB_DSN 覆盖。
func analysisTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/quant_trading?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping: cannot create pool for %s: %v", dsn, err)
		return nil
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping: cannot reach database at %s: %v", dsn, err)
		return nil
	}
	var reg *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('users')::text`).Scan(&reg); err != nil || reg == nil {
		pool.Close()
		t.Skipf("skipping: users table not present in %s", dsn)
		return nil
	}
	t.Cleanup(pool.Close)
	return pool
}

func requireEmptyUsersForAnalysis(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n))
	if n != 0 {
		t.Skipf("skipping: users 表已有 %d 行，窗口在本库上已经关了", n)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users`); err != nil {
			t.Logf("cleanup: 清空 users 失败: %v", err)
		}
	})
}
