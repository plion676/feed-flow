package jwt

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidConfig is returned when the JWT manager is created with bad config.
var ErrInvalidConfig = errors.New("invalid jwt config")

// ErrInvalidUserID indicates an illegal user identifier for token generation.
var ErrInvalidUserID = errors.New("invalid user id")

// ErrInvalidToken indicates JWT parsing or verification failure.
var ErrInvalidToken = errors.New("invalid token")

// ErrInvalidClaims indicates required claims are missing or malformed.
var ErrInvalidClaims = errors.New("invalid token claims")

// Config is the minimal configuration required by JWT token generation.
type Config struct {
	Secret      string
	ExpireHours int
}

// Manager owns JWT-related operations and keeps token config in one place.
type Manager struct {
	secret      string
	expireHours int
}

// Claims defines the JWT payload used by this project.
type Claims struct {
	UserID int64 `json:"user_id"`
	jwt.RegisteredClaims
}

// NewManager validates and stores JWT configuration for later token operations.
func NewManager(cfg Config) (*Manager, error) {
	if strings.TrimSpace(cfg.Secret) == "" || cfg.ExpireHours <= 0 {
		return nil, ErrInvalidConfig
	}

	return &Manager{
		secret:      cfg.Secret,
		expireHours: cfg.ExpireHours,
	}, nil
}

// GenerateToken builds and signs a JWT for an authenticated user.
func (m *Manager) GenerateToken(userID int64) (string, error) {
	if userID <= 0 {
		return "", ErrInvalidUserID
	}
	if strings.TrimSpace(m.secret) == "" || m.expireHours <= 0 {
		return "", ErrInvalidConfig
	}

	now := time.Now()
	expireAt := now.Add(time.Duration(m.expireHours) * time.Hour)

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expireAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(m.secret))
}

// ParseToken verifies a JWT and returns strongly-typed claims.
func (m *Manager) ParseToken(tokenString string) (*Claims, error) {
	if strings.TrimSpace(tokenString) == "" {
		return nil, ErrInvalidToken
	}
	if strings.TrimSpace(m.secret) == "" || m.expireHours <= 0 {
		return nil, ErrInvalidConfig
	}

	parsedToken, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, ErrInvalidToken
			}
			return []byte(m.secret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer("feed-flow"),
	)
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := parsedToken.Claims.(*Claims)
	if !ok || !parsedToken.Valid {
		return nil, ErrInvalidToken
	}
	if claims.UserID <= 0 {
		return nil, ErrInvalidClaims
	}

	return claims, nil
}
