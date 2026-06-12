package auth

import (
	"fmt"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload we sign. We extend RegisteredClaims with the
// minimum information downstream handlers need: username, role, and a kind
// discriminator (access vs refresh) so we can refuse to use a refresh token
// as an access token.
type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Kind     string `json:"kind"` // "access" | "refresh"
	jwt.RegisteredClaims
}

// UserIDInt64 returns the subject as int64. Returns an error if the subject
// is not a valid integer.
func (c *Claims) UserIDInt64() (int64, error) {
	if c.Subject == "" {
		return 0, fmt.Errorf("auth: token has empty subject")
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("auth: subject is not an int64: %w", err)
	}
	return id, nil
}
