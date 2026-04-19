package xerror

// Error represents a stable business or infrastructure error.
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

const (
	CodeOK                 = 0
	CodeBadRequest         = 1001
	CodeUnauthorized       = 1002
	CodeInternal           = 1003
	CodeNotImplemented     = 1004
	CodeInvalidCredentials = 2002
	CodeUserAlreadyExists  = 2001
)

var (
	ErrBadRequest         = &Error{Code: CodeBadRequest, Message: "bad request"}
	ErrUnauthorized       = &Error{Code: CodeUnauthorized, Message: "unauthorized"}
	ErrInternal           = &Error{Code: CodeInternal, Message: "internal server error"}
	ErrNotImplemented     = &Error{Code: CodeNotImplemented, Message: "not implemented yet"}
	ErrInvalidCredentials = &Error{Code: CodeInvalidCredentials, Message: "invalid username or password"}
	ErrUserAlreadyExists  = &Error{Code: CodeUserAlreadyExists, Message: "username already exists"}
)
