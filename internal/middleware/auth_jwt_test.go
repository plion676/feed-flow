package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakeTokenParser struct {
	claims *jwtpkg.Claims
	err    error
}

func (p *fakeTokenParser) ParseToken(_ string) (*jwtpkg.Claims, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.claims, nil
}

type errorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func TestAuthJWTHeaderValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		header string
	}{
		{name: "missing authorization header", header: ""},
		{name: "wrong auth scheme", header: "Token abc.def.ghi"},
		{name: "empty bearer token", header: "Bearer   "},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			router := gin.New()
			router.Use(AuthJWT(&fakeTokenParser{
				claims: &jwtpkg.Claims{UserID: 1001},
			}))
			router.GET("/protected", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusUnauthorized)
			}

			var body errorResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response failed: %v", err)
			}
			if body.Code != xerror.CodeUnauthorized {
				t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeUnauthorized)
			}
		})
	}
}

func TestAuthJWTTokenValidation(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	manager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "test-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	validToken, err := manager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate valid token failed: %v", err)
	}

	expiredToken, err := signTokenForTest("test-secret", 1001, time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("generate expired token failed: %v", err)
	}

	wrongSignatureToken, err := signTokenForTest("other-secret", 1001, time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("generate wrong-signature token failed: %v", err)
	}

	tests := []struct {
		name       string
		parser     tokenParser
		header     string
		wantStatus int
		wantUserID int64
	}{
		{
			name:       "parser returns error",
			parser:     &fakeTokenParser{err: errors.New("parse failed")},
			header:     "Bearer any-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "expired token",
			parser:     manager,
			header:     "Bearer " + expiredToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong signature token",
			parser:     manager,
			header:     "Bearer " + wrongSignatureToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "valid token",
			parser:     manager,
			header:     "Bearer " + validToken,
			wantStatus: http.StatusOK,
			wantUserID: 1001,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotUserID int64

			router := gin.New()
			router.Use(AuthJWT(tc.parser))
			router.GET("/protected", func(c *gin.Context) {
				userID, ok := CurrentUserID(c)
				if ok {
					gotUserID = userID
				}
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", tc.header)

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != tc.wantStatus {
				t.Fatalf("unexpected status: got=%d want=%d", resp.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK && gotUserID != tc.wantUserID {
				t.Fatalf("unexpected user id: got=%d want=%d", gotUserID, tc.wantUserID)
			}
		})
	}
}

func signTokenForTest(secret string, userID int64, issuedAt time.Time, expiresAt time.Time) (string, error) {
	claims := jwtpkg.Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "feed-flow",
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
