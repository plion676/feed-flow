package jwt

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestNewManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{
			name: "invalid empty secret",
			cfg: Config{
				Secret:      " ",
				ExpireHours: 1,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "invalid expire hours",
			cfg: Config{
				Secret:      "secret",
				ExpireHours: 0,
			},
			wantErr: ErrInvalidConfig,
		},
		{
			name: "valid config",
			cfg: Config{
				Secret:      "secret",
				ExpireHours: 24,
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manager, err := NewManager(tc.cfg)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("unexpected error: got=%v want=%v", err, tc.wantErr)
			}
			if tc.wantErr == nil && manager == nil {
				t.Fatal("manager should not be nil when config is valid")
			}
		})
	}
}

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(Config{
		Secret:      "test-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new manager failed: %v", err)
	}

	_, err = manager.GenerateToken(0)
	if !errors.Is(err, ErrInvalidUserID) {
		t.Fatalf("unexpected error for invalid user id: %v", err)
	}

	token, err := manager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
}

func TestParseToken(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(Config{
		Secret:      "test-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new manager failed: %v", err)
	}

	now := time.Now()
	validToken, err := signTokenForJWTTest(jwt.SigningMethodHS256, "test-secret", Claims{
		UserID: 1001,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign valid token failed: %v", err)
	}

	expiredToken, err := signTokenForJWTTest(jwt.SigningMethodHS256, "test-secret", Claims{
		UserID: 1001,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now.Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign expired token failed: %v", err)
	}

	wrongIssuerToken, err := signTokenForJWTTest(jwt.SigningMethodHS256, "test-secret", Claims{
		UserID: 1001,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "other-service",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign wrong-issuer token failed: %v", err)
	}

	wrongSignatureToken, err := signTokenForJWTTest(jwt.SigningMethodHS256, "other-secret", Claims{
		UserID: 1001,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign wrong-signature token failed: %v", err)
	}

	wrongAlgToken, err := signTokenForJWTTest(jwt.SigningMethodHS384, "test-secret", Claims{
		UserID: 1001,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign wrong-algorithm token failed: %v", err)
	}

	invalidClaimsToken, err := signTokenForJWTTest(jwt.SigningMethodHS256, "test-secret", Claims{
		UserID: 0,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("sign invalid-claims token failed: %v", err)
	}

	tests := []struct {
		name     string
		token    string
		wantErr  error
		wantUser int64
	}{
		{
			name:    "empty token",
			token:   " ",
			wantErr: ErrInvalidToken,
		},
		{
			name:    "expired token",
			token:   expiredToken,
			wantErr: ErrInvalidToken,
		},
		{
			name:    "wrong issuer token",
			token:   wrongIssuerToken,
			wantErr: ErrInvalidToken,
		},
		{
			name:    "wrong signature token",
			token:   wrongSignatureToken,
			wantErr: ErrInvalidToken,
		},
		{
			name:    "wrong algorithm token",
			token:   wrongAlgToken,
			wantErr: ErrInvalidToken,
		},
		{
			name:    "invalid claims token",
			token:   invalidClaimsToken,
			wantErr: ErrInvalidClaims,
		},
		{
			name:     "valid token",
			token:    validToken,
			wantErr:  nil,
			wantUser: 1001,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			claims, err := manager.ParseToken(tc.token)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("unexpected error: got=%v want=%v", err, tc.wantErr)
			}

			if tc.wantErr != nil {
				if claims != nil {
					t.Fatalf("claims should be nil when error happens, got=%+v", claims)
				}
				return
			}

			if claims == nil {
				t.Fatal("claims should not be nil")
			}
			if claims.UserID != tc.wantUser {
				t.Fatalf("unexpected user id: got=%d want=%d", claims.UserID, tc.wantUser)
			}
		})
	}
}

func signTokenForJWTTest(method jwt.SigningMethod, secret string, claims Claims) (string, error) {
	token := jwt.NewWithClaims(method, claims)
	return token.SignedString([]byte(secret))
}
