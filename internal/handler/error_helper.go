package handler

import (
	"net/http"

	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

func responseBindError() *xerror.Error {
	return xerror.ErrBadRequest
}

func httpStatusFromError(err *xerror.Error) int {
	switch err {
	case xerror.ErrBadRequest:
		return http.StatusBadRequest
	case xerror.ErrUnauthorized:
		return http.StatusUnauthorized
	case xerror.ErrForbidden:
		return http.StatusForbidden
	case xerror.ErrInvalidCredentials:
		return http.StatusUnauthorized
	case xerror.ErrUserAlreadyExists:
		return http.StatusConflict
	case xerror.ErrNotFound:
		return http.StatusNotFound
	case xerror.ErrPostNotFound:
		return http.StatusNotFound
	case xerror.ErrFollowAlreadyExists:
		return http.StatusConflict
	case xerror.ErrNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}
