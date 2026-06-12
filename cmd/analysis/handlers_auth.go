package main

import (
	"errors"
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "username and password required"})
			return
		}
		u, err := svc.Authenticate(c.Request.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrUserDisabled) {
				c.JSON(http.StatusForbidden, gin.H{"error": "account disabled"})
				return
			}
			// Bad creds, unknown user, etc. — collapse to a single
			// 401 with a generic message to avoid leaking which
			// usernames exist.
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}
		access, refresh, err := svc.IssueTokens(u)
		if err != nil {
			logger.Error().Err(err).Msg("auth: IssueTokens failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "token issuance failed"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token required"})
			return
		}
		access, refresh, err := svc.Refresh(req.RefreshToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
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

func meHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid, role, ok := auth.UserFromContext(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "username, password (≥8), and role required"})
			return
		}
		role := auth.Role(req.Role)
		if !role.IsValid() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be one of viewer/trader/admin"})
			return
		}
		u, err := svc.CreateUser(c.Request.Context(), req.Username, req.Password, role)
		if err != nil {
			if errors.Is(err, auth.ErrUserExists) {
				c.JSON(http.StatusConflict, gin.H{"error": "username already taken"})
				return
			}
			logger.Error().Err(err).Str("username", req.Username).Msg("create user failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "create user failed"})
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"users": users})
	}
}

func listAuditHandler(svc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := svc.ListAudit(c.Request.Context(), 100)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"audit": rows})
	}
}
