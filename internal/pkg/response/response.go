package response

import (
	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

// CommonResponse is the shared HTTP response envelope.
type CommonResponse struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestID string      `json:"request_id,omitempty"`
}

func Success(c *gin.Context, httpStatus int, data interface{}) {
	JSON(c, httpStatus, xerror.CodeOK, "success", data)
}

func Fail(c *gin.Context, httpStatus int, err *xerror.Error) {
	JSON(c, httpStatus, err.Code, err.Message, nil)
}

func JSON(c *gin.Context, httpStatus int, code int, message string, data interface{}) {
	c.JSON(httpStatus, CommonResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: RequestID(c),
	})
}

func RequestID(c *gin.Context) string {
	requestID, exists := c.Get("request_id")
	if !exists {
		return ""
	}

	id, ok := requestID.(string)
	if !ok {
		return ""
	}

	return id
}
