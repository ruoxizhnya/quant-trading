// Package auth provides JWT-based authentication, RBAC, and audit log support.
//
// Design (ADR-017 §2, ODR-019):
//   - User credentials are bcrypt-hashed and stored in the `users` table.
//   - Successful login issues a short-lived access token (default 15 min) and
//     a longer-lived refresh token (default 7 days). Both are HS256 JWTs.
//   - The middleware verifies the access token, extracts the user identity
//     and role, and stores them in the gin.Context for downstream handlers.
//   - The audit log middleware records every mutating API call (POST/PUT/DELETE)
//     to the `audit_logs` table for compliance and debugging.
//
// All three layers (auth, RBAC, audit) are designed to be optional — if no
// `JWTSecret` is configured, the middleware is a no-op and the system runs in
// the legacy "open access" mode used by the dev / test environments. This
// keeps backward compatibility while letting ops turn on auth by setting a
// single environment variable.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Role is a coarse-grained access tier.
type Role string

const (
	RoleViewer Role = "viewer" // read-only
	RoleTrader Role = "trader" // can place paper/live orders
	RoleAdmin  Role = "admin"  // full access incl. user management
)

// AllRoles returns every defined role. Used for validation.
func AllRoles() []Role {
	return []Role{RoleViewer, RoleTrader, RoleAdmin}
}

// IsValid reports whether r is one of the known roles.
func (r Role) IsValid() bool {
	for _, x := range AllRoles() {
		if x == r {
			return true
		}
	}
	return false
}

// CanTrade reports whether a role is allowed to submit orders.
func (r Role) CanTrade() bool {
	return r == RoleTrader || r == RoleAdmin
}

// CanAdmin reports whether a role is allowed to manage users / view audit logs.
func (r Role) CanAdmin() bool {
	return r == RoleAdmin
}

// User is a row in the `users` table.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         Role
	CreatedAt    time.Time
	LastLoginAt  *time.Time
	Disabled     bool
}

// AuditLog is a row in the `audit_logs` table.
type AuditLog struct {
	ID          int64
	UserID      *int64
	Role        string
	IP          string
	Endpoint    string
	Method      string
	PayloadHash string
	TraceID     string
	StatusCode  int
	Timestamp   time.Time
}

// Sentinel errors.
var (
	ErrUserNotFound       = errors.New("auth: user not found")
	ErrUserExists         = errors.New("auth: username already taken")
	ErrInvalidCredentials = errors.New("auth: invalid username or password")
	ErrInvalidToken       = errors.New("auth: invalid or expired token")
	ErrUserDisabled       = errors.New("auth: user account is disabled")
	ErrInvalidRole        = errors.New("auth: invalid role")
)

// BcryptCost is the work factor used for new password hashes. 12 is the
// 2026 industry default — slow enough to discourage offline cracking on
// consumer GPUs, fast enough to not tank login latency.
const BcryptCost = 12

// Config is the static configuration for the auth subsystem.
type Config struct {
	JWTSecret       []byte
	AccessTokenTTL  time.Duration // default 15m
	RefreshTokenTTL time.Duration // default 7d
	Issuer          string        // default "quant-trading"
}

// WithDefaults returns c with zero-value fields replaced by sensible defaults.
func (c Config) WithDefaults() Config {
	if c.AccessTokenTTL == 0 {
		c.AccessTokenTTL = 15 * time.Minute
	}
	if c.RefreshTokenTTL == 0 {
		c.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if c.Issuer == "" {
		c.Issuer = "quant-trading"
	}
	return c
}

// Service is the auth entry point. It is safe for concurrent use.
type Service struct {
	cfg    Config
	pool   *pgxpool.Pool
	secret []byte
}

// NewService creates a Service backed by the given pgxpool.
//
// If cfg.JWTSecret is empty, the service is created in "disabled" mode:
// the auth middleware is a no-op and only the password / RBAC helpers
// remain usable. This keeps dev environments friction-free.
func NewService(pool *pgxpool.Pool, cfg Config) *Service {
	cfg = cfg.WithDefaults()
	return &Service{
		cfg:    cfg,
		pool:   pool,
		secret: cfg.JWTSecret,
	}
}

// Enabled reports whether JWT issuance / verification is on.
func (s *Service) Enabled() bool {
	return len(s.secret) > 0
}

// AccessTTL returns the configured access token TTL. Used by handlers to
// populate the `expires_in` field of the token response.
func (s *Service) AccessTTL() time.Duration {
	return s.cfg.AccessTokenTTL
}

// ListUsers returns up to `limit` user records (without password hashes).
func (s *Service) ListUsers(ctx context.Context, limit int) ([]User, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, role, created_at, last_login_at, disabled
		FROM users ORDER BY id ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var lastLogin *time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &lastLogin, &u.Disabled); err != nil {
			return nil, err
		}
		u.LastLoginAt = lastLogin
		// Empty out the password hash so it never leaks through the API.
		u.PasswordHash = ""
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateUser inserts a new user. The plaintext password is bcrypt-hashed
// before being stored; the original is never persisted or logged.
func (s *Service) CreateUser(ctx context.Context, username, password string, role Role) (*User, error) {
	if !role.IsValid() {
		return nil, ErrInvalidRole
	}
	if username == "" || len(password) < 8 {
		return nil, fmt.Errorf("auth: username required and password must be ≥8 chars")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("auth: bcrypt failed: %w", err)
	}
	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (username) DO NOTHING
		RETURNING id`,
		username, string(hash), string(role),
	).Scan(&id)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("auth: insert user: %w", err)
	}
	return s.GetUserByID(ctx, id)
}

// isNoRows reports whether err is a pgx "no rows" error. We can't import
// pgx's error directly without polluting the public surface, so we match
// on the message — it's been stable since pgx v4.
func isNoRows(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "no rows in result set" || msg == "ErrNoRows"
}

// Authenticate verifies the username/password and returns the user on success.
// On any failure (unknown user, bad password, disabled account) it returns
// ErrInvalidCredentials / ErrUserDisabled to avoid leaking which usernames exist.
func (s *Service) Authenticate(ctx context.Context, username, password string) (*User, error) {
	u, err := s.getUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if u.Disabled {
		return nil, ErrUserDisabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	// Best-effort last_login_at update. We don't fail the login if this fails.
	_, _ = s.pool.Exec(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, u.ID)
	now := time.Now().UTC()
	u.LastLoginAt = &now
	return u, nil
}

// GetUserByID returns the user with the given ID, or ErrUserNotFound.
func (s *Service) GetUserByID(ctx context.Context, id int64) (*User, error) {
	u := &User{}
	var lastLogin *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, last_login_at, disabled
		FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &lastLogin, &u.Disabled)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	u.LastLoginAt = lastLogin
	return u, nil
}

func (s *Service) getUserByUsername(ctx context.Context, username string) (*User, error) {
	u := &User{}
	var lastLogin *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, role, created_at, last_login_at, disabled
		FROM users WHERE username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &lastLogin, &u.Disabled)
	if err != nil {
		if isNoRows(err) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	u.LastLoginAt = lastLogin
	return u, nil
}

// IssueTokens mints a fresh access + refresh token pair for the user.
func (s *Service) IssueTokens(u *User) (access, refresh string, err error) {
	if !s.Enabled() {
		return "", "", errors.New("auth: cannot issue tokens: JWT secret not configured")
	}
	now := time.Now().UTC()
	accessTTL := s.cfg.AccessTokenTTL
	refreshTTL := s.cfg.RefreshTokenTTL

	accessClaims := Claims{
		Username: u.Username,
		Role:     string(u.Role),
		Kind:     "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", u.ID),
			Issuer:    s.cfg.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
		},
	}
	refreshClaims := Claims{
		Username: u.Username,
		Role:     string(u.Role),
		Kind:     "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", u.ID),
			Issuer:    s.cfg.Issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(refreshTTL)),
		},
	}

	access, err = s.mint(accessClaims)
	if err != nil {
		return "", "", err
	}
	refresh, err = s.mint(refreshClaims)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// Refresh validates a refresh token and mints a new access token (and rotates
// the refresh token). Returns ErrInvalidToken for any kind of failure.
func (s *Service) Refresh(refreshToken string) (access, refresh string, err error) {
	claims, err := s.Parse(refreshToken)
	if err != nil {
		return "", "", ErrInvalidToken
	}
	if claims.Kind != "refresh" {
		return "", "", ErrInvalidToken
	}
	uid, err := claims.UserIDInt64()
	if err != nil {
		return "", "", ErrInvalidToken
	}
	u, err := s.GetUserByID(context.Background(), uid)
	if err != nil {
		return "", "", ErrInvalidToken
	}
	if u.Disabled {
		return "", "", ErrUserDisabled
	}
	return s.IssueTokens(u)
}

func (s *Service) mint(c Claims) (string, error) {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString(s.secret)
}

// Parse validates the token's signature and expiry and returns the claims.
func (s *Service) Parse(token string) (*Claims, error) {
	if !s.Enabled() {
		return nil, ErrInvalidToken
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	var c Claims
	t, err := parser.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil || !t.Valid {
		return nil, ErrInvalidToken
	}
	return &c, nil
}

// RecordAudit persists an audit log entry. Failures are returned but the
// HTTP middleware logs and continues (a broken audit log should not take
// down the API).
func (s *Service) RecordAudit(ctx context.Context, entry AuditLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_logs
			(user_id, role, ip, endpoint, method, payload_hash, trace_id, status_code)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.UserID, entry.Role, entry.IP, entry.Endpoint, entry.Method,
		entry.PayloadHash, entry.TraceID, entry.StatusCode,
	)
	return err
}

// ListAudit returns the most recent audit log rows, newest first. Admin only.
func (s *Service) ListAudit(ctx context.Context, limit int) ([]AuditLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, role, ip::text, endpoint, method, payload_hash, trace_id, status_code, timestamp
		FROM audit_logs
		ORDER BY timestamp DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var e AuditLog
		if err := rows.Scan(&e.ID, &e.UserID, &e.Role, &e.IP, &e.Endpoint, &e.Method,
			&e.PayloadHash, &e.TraceID, &e.StatusCode, &e.Timestamp); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
