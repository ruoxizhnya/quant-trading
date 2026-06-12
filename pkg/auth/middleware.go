package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Context keys for storing identity info on *gin.Context.
const (
	CtxUserID   = "auth.user_id"
	CtxUsername = "auth.username"
	CtxRole     = "auth.role"
	CtxClaims   = "auth.claims"
)

// Middleware returns a gin middleware that enforces JWT auth. If the
// service is not enabled (no JWT secret), the middleware is a no-op so
// dev environments continue to work.
//
// On success the middleware stores the user ID, username, and role in the
// gin.Context so downstream handlers can read them via UserFromContext.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.Enabled() {
			c.Next()
			return
		}
		// Skip auth for the login endpoint itself. The caller can use
		// the path matcher; we just check the route.
		if isPublicPath(c.Request.URL.Path) {
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "missing Authorization header",
			})
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header must be 'Bearer <token>'",
			})
			return
		}
		claims, err := s.Parse(parts[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			return
		}
		if claims.Kind != "access" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "refresh token cannot be used as access token",
			})
			return
		}
		uid, err := claims.UserIDInt64()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "malformed token subject",
			})
			return
		}
		c.Set(CtxUserID, uid)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, Role(claims.Role))
		c.Set(CtxClaims, claims)
		c.Next()
	}
}

// RequireRole returns a middleware that aborts with 403 if the caller's
// role is not in the allowed set. Place AFTER Middleware().
func RequireRole(allowed ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := RoleFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "no authenticated user (RequireRole placed before Middleware?)",
			})
			return
		}
		for _, r := range allowed {
			if r == role {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "insufficient role",
			"have":  string(role),
			"need":  rolesToStrings(allowed),
		})
	}
}

// AuditMiddleware records every mutating request (POST/PUT/DELETE/PATCH) to
// the audit_logs table. The body is hashed (not stored) so we keep a
// fingerprint without leaking PII.
//
// Place AFTER Middleware() so the user_id / role are populated. Failures to
// write the audit row are logged and swallowed; a broken audit log should
// not take down the API.
func (s *Service) AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Capture body so we can hash it. The original request body is
		// restored so downstream handlers can still read it.
		var bodyHash string
		if c.Request.Body != nil && isMutating(c.Request.Method) {
			body, err := io.ReadAll(c.Request.Body)
			if err == nil {
				h := sha256.Sum256(body)
				bodyHash = hex.EncodeToString(h[:])
				// Re-wrap the body so handlers can re-read it.
				c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
			}
		}

		c.Next()

		// Only log mutating methods. GET requests are too noisy and not
		// what the audit log is for.
		if !isMutating(c.Request.Method) {
			return
		}

		entry := AuditLog{
			IP:          c.ClientIP(),
			Endpoint:    c.Request.URL.Path,
			Method:      c.Request.Method,
			PayloadHash: bodyHash,
			TraceID:     c.GetHeader("X-Request-ID"),
			StatusCode:  c.Writer.Status(),
		}
		if uid, ok := c.Get(CtxUserID); ok {
			if v, ok := uid.(int64); ok {
				entry.UserID = &v
			}
		}
		if role, ok := c.Get(CtxRole); ok {
			if v, ok := role.(Role); ok {
				entry.Role = string(v)
			}
		}

		if err := s.RecordAudit(c.Request.Context(), entry); err != nil {
			// Best-effort. Log to stderr via gin if we can; otherwise
			// silently swallow. Audit loss is bad but not catastrophic.
			c.Error(err) // attach to gin's error chain
		}
	}
}

// UserFromContext returns the authenticated user ID and role, or false if
// the request is unauthenticated (or the middleware is disabled).
func UserFromContext(c *gin.Context) (int64, Role, bool) {
	uidVal, uidOK := c.Get(CtxUserID)
	roleVal, roleOK := c.Get(CtxRole)
	if !uidOK || !roleOK {
		return 0, "", false
	}
	uid, ok1 := uidVal.(int64)
	role, ok2 := roleVal.(Role)
	return uid, role, ok1 && ok2
}

// RoleFromContext returns just the role, or "" / false.
func RoleFromContext(c *gin.Context) (Role, bool) {
	v, ok := c.Get(CtxRole)
	if !ok {
		return "", false
	}
	r, ok := v.(Role)
	return r, ok
}

// isPublicPath returns true for routes that are always unauthenticated
// (login, refresh, health, metrics). Keep this list in sync with the
// routes registered in main.go.
func isPublicPath(path string) bool {
	public := []string{
		"/api/auth/login",
		"/api/auth/refresh",
		"/health",
		"/api/health",
		"/metrics",
	}
	for _, p := range public {
		if path == p {
			return true
		}
	}
	return false
}

// isMutating reports whether the HTTP method is one that changes state.
func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func rolesToStrings(rs []Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// ErrUnauthenticated is returned by helpers when the context has no user.
var ErrUnauthenticated = errors.New("auth: request has no authenticated user")
