package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
	"github.com/plion676/feed-flow/internal/pkg/response"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

const currentUserIDKey = "current_user_id"

type tokenParser interface {
	ParseToken(tokenString string) (*jwtpkg.Claims, error)
}

// AuthJWT validates Bearer tokens and stores user id in request context.
func AuthJWT(parser tokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
		if authHeader == "" {
			response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
			c.Abort()
			return
		}

		claims, err := parser.ParseToken(strings.TrimSpace(parts[1]))
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
			c.Abort()
			return
		}

		c.Set(currentUserIDKey, claims.UserID)
		c.Next()
	}
}

// CurrentUserID extracts authenticated user id from Gin context.
func CurrentUserID(c *gin.Context) (int64, bool) {
	value, exists := c.Get(currentUserIDKey)
	if !exists {
		return 0, false
	}

	userID, ok := value.(int64)
	if !ok || userID <= 0 {
		return 0, false
	}

	return userID, true
}
