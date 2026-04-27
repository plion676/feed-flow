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
	CodeOK                  = 0
	CodeBadRequest          = 1001
	CodeUnauthorized        = 1002
	CodeInternal            = 1003
	CodeNotImplemented      = 1004
	CodeNotFound            = 1005
	CodeForbidden           = 1006
	CodeInvalidCredentials  = 2002
	CodeUserAlreadyExists   = 2001
	CodePostNotFound        = 3001
	CodeFollowAlreadyExists = 3002
)

var (
	ErrBadRequest          = &Error{Code: CodeBadRequest, Message: "bad request"}
	ErrUnauthorized        = &Error{Code: CodeUnauthorized, Message: "unauthorized"}
	ErrForbidden           = &Error{Code: CodeForbidden, Message: "forbidden"}
	ErrInternal            = &Error{Code: CodeInternal, Message: "internal server error"}
	ErrNotImplemented      = &Error{Code: CodeNotImplemented, Message: "not implemented yet"}
	ErrNotFound            = &Error{Code: CodeNotFound, Message: "resource not found"}
	ErrInvalidCredentials  = &Error{Code: CodeInvalidCredentials, Message: "invalid username or password"}
	ErrUserAlreadyExists   = &Error{Code: CodeUserAlreadyExists, Message: "username already exists"}
	ErrPostNotFound        = &Error{Code: CodePostNotFound, Message: "post not found"}
	ErrFollowAlreadyExists = &Error{Code: CodeFollowAlreadyExists, Message: "already followed"}
)
