package main

import (
	"errors"
	"github.com/ruoxizhnya/quant-trading/internal/httpserver"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/ruoxizhnya/quant-trading/pkg/auth"
)

// registerAuthRoutes wires the /api/auth/* endpoints (login, refresh, me,
// admin user management) onto the router. The auth service is responsible
// for issuing and validating JWTs; the handlers are thin.
func registerAuthRoutes(router *gin.Engine, svc *auth.Service, logger zerolog.Logger) {
	g := router.Group("/api/auth")
	{
		g.POST("/login", loginHandler(svc, logger))
		g.POST("/refresh", refreshHandler(svc, logger))
		// Both of these are deliberately public. See their doc comments: the
		// SPA cannot decide whether to render a login page before it knows the
		// posture, and a first-run instance has no credential to ask with.
		g.GET("/status", authStatusHandler(svc))
		g.POST("/bootstrap", bootstrapHandler(svc, logger))
	}

	// Authenticated self-service endpoints.
	authd := router.Group("/api/auth", svc.Middleware())
	{
		authd.GET("/me", meHandler(svc))
	}

	// Admin-only user management.
	admin := router.Group("/api/auth/admin", svc.Middleware(), auth.RequireRole(auth.RoleAdmin))
	{
		admin.POST("/users", createUserHandler(svc, logger))
		admin.GET("/users", listUsersHandler(svc))
		admin.GET("/audit", listAuditHandler(svc))
	}
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // seconds, for the access token
	Username     string `json:"username"`
	Role         string `json:"role"`
}

func loginHandler(svc *auth.Service, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "username and password required")
			return
		}
		u, err := svc.Authenticate(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrUserDisabled) {
				httpserver.Fail(c, http.StatusForbidden, "account disabled")
				return
			}
			// Bad creds, unknown user, etc. — collapse to a single
			// 401 with a generic message to avoid leaking which
			// usernames exist.
			httpserver.Fail(c, http.StatusUnauthorized, "invalid username or password")
			return
		}
		access, refresh, err := svc.IssueTokens(u)
		if err != nil {
			logger.Error().Err(err).Msg("auth: IssueTokens failed")
			httpserver.Fail(c, http.StatusInternalServerError, "token issuance failed")
			return
		}
		c.JSON(http.StatusOK, tokenResponse{
			AccessToken:  access,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			ExpiresIn:    int(svc.AccessTTL().Seconds()),
			Username:     u.Username,
			Role:         string(u.Role),
		})
	}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func refreshHandler(svc *auth.Service, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "refresh_token required")
			return
		}
		access, refresh, err := svc.Refresh(req.RefreshToken)
		if err != nil {
			httpserver.Fail(c, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		c.JSON(http.StatusOK, tokenResponse{
			AccessToken:  access,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			ExpiresIn:    int(svc.AccessTTL().Seconds()),
		})
	}
}

// authStatusResponse is the payload of GET /api/auth/status. Two booleans, no
// identifiers: it deliberately says nothing about *who* exists.
type authStatusResponse struct {
	AuthEnabled       bool `json:"auth_enabled"`
	BootstrapRequired bool `json:"bootstrap_required"`
}

// authStatusHandler serves GET /api/auth/status.
//
// Public by necessity, not by convenience. The SPA has to choose between
// rendering a login form and rendering a "create the first administrator" form
// *before* it holds any credential — the branch it takes is exactly "do I have a
// token", and the answer to "how do I get one" depends on this endpoint. Any
// design that gates it behind a token is unenterable on a fresh instance.
//
// It discloses one bit that is already externally observable — whether the
// instance is open-access — plus whether the first-admin window is still open.
// Neither is a secret: an open-access instance answers every /api/* call with
// 200 to anyone, and a not-yet-bootstrapped instance is by definition one that
// has never been used.
//
// When auth is disabled this answers without touching the database (see
// auth.Service.BootstrapRequired), which keeps the dev/CI posture working on a
// machine with no database at all. When auth is enabled but the database is
// unreachable it returns 500 rather than guessing: the SPA must fail closed
// (assume auth is on, show the login form), and a made-up 200 would send it
// chasing a login that cannot succeed.
func authStatusHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !svc.Enabled() {
			c.JSON(http.StatusOK, authStatusResponse{AuthEnabled: false, BootstrapRequired: false})
			return
		}
		need, err := svc.BootstrapRequired(c.Request.Context())
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, authStatusResponse{AuthEnabled: true, BootstrapRequired: need})
	}
}

type bootstrapRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=8"`
}

// bootstrapHandler serves POST /api/auth/bootstrap: the one-time creation of
// the first administrator.
//
// This is the only unauthenticated write path in the system. The authorisation
// is not a credential but a state — the `users` table being empty — and that
// state is checked inside a transaction by auth.Service.CreateFirstAdmin.
// Whoever reaches the instance first can claim admin; that is inherent to
// self-service bootstrap, and it is why the local stack publishes on loopback
// only (ADR-025).
//
// On success the caller is logged in immediately. Round-tripping back through
// /login would add no security (the caller just proved it can reach an unclaimed
// instance) and one more way to get stuck — e.g. a mistyped username, which
// would send it to /login with credentials that do not exist.
//
// This path *is* covered by AuditMiddleware, so every bootstrap attempt that
// gets past the middleware leaves an audit_logs row (user_id NULL on failure to
// authenticate; the endpoint and method are what identify it).
func bootstrapHandler(svc *auth.Service, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req bootstrapRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "username and password (≥8 chars) required")
			return
		}
		u, err := svc.CreateFirstAdmin(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrBootstrapClosed) {
				// One message for both "someone already exists" and "auth is
				// disabled": neither is actionable by the caller, and naming
				// which one it is would tell an anonymous caller whether any
				// account exists.
				httpserver.Fail(c, http.StatusForbidden, "bootstrap window is closed")
				return
			}
			logger.Error().Err(err).Msg("auth: bootstrap failed")
			httpserver.Fail(c, http.StatusInternalServerError, "bootstrap failed")
			return
		}
		access, refresh, err := svc.IssueTokens(u)
		if err != nil {
			// The admin exists now; only the token mint failed. Report it
			// plainly and let the caller log in normally rather than rolling
			// the account back.
			logger.Error().Err(err).Msg("auth: IssueTokens after bootstrap failed")
			httpserver.Fail(c, http.StatusInternalServerError, "account created but token issuance failed; please log in")
			return
		}
		logger.Warn().Str("username", u.Username).Msg("auth: first administrator bootstrapped")
		c.JSON(http.StatusCreated, tokenResponse{
			AccessToken:  access,
			RefreshToken: refresh,
			TokenType:    "Bearer",
			ExpiresIn:    int(svc.AccessTTL().Seconds()),
			Username:     u.Username,
			Role:         string(u.Role),
		})
	}
}

func meHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, role, ok := auth.UserFromContext(c)
		if !ok {
			httpserver.Fail(c, http.StatusUnauthorized, "not authenticated")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id":  uid,
			"username": c.GetString(auth.CtxUsername),
			"role":     string(role),
		})
	}
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=8"`
	Role     string `json:"role" binding:"required"`
}

func createUserHandler(svc *auth.Service, logger zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			httpserver.Fail(c, http.StatusBadRequest, "username, password (≥8), and role required")
			return
		}
		role := auth.Role(req.Role)
		if !role.IsValid() {
			httpserver.Fail(c, http.StatusBadRequest, "role must be one of viewer/trader/admin")
			return
		}
		u, err := svc.CreateUser(c.Request.Context(), req.Username, req.Password, role)
		if err != nil {
			if errors.Is(err, auth.ErrUserExists) {
				httpserver.Fail(c, http.StatusConflict, "username already taken")
				return
			}
			logger.Error().Err(err).Str("username", req.Username).Msg("create user failed")
			httpserver.Fail(c, http.StatusInternalServerError, "create user failed")
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"user_id":  u.ID,
			"username": u.Username,
			"role":     string(u.Role),
		})
	}
}

func listUsersHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		users, err := svc.ListUsers(c.Request.Context(), 100)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"users": users})
	}
}

func listAuditHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.ListAudit(c.Request.Context(), 100)
		if err != nil {
			httpserver.Error(c, http.StatusInternalServerError, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"audit": rows})
	}
}
