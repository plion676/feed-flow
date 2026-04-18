package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/pkg/response"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

// Recovery catches panics and converts them to the unified error response.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic recovered: %v", rec)
				response.Fail(c, http.StatusInternalServerError, xerror.ErrInternal)
				c.Abort()
			}
		}()

		c.Next()
	}
}
