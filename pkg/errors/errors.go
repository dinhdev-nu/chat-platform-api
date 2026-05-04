package errors

import (
	"errors"
	"fmt"
)

type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Err     error     `json:"error,omitempty"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func (e *AppError) HttpStatus() int {
	return e.Code.HttpStatus()
}

// errors.New() Dùng khi: biết rõ lỗi gì, không có raw error
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// errors.Wrap(Code, message, err) Dùng khi: có raw error từ db/external service cần log
func Wrap(code ErrorCode, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// errors.NotFound("user")        → code=NOT_FOUND,  message="user not found"
func NotFound(message string) *AppError {
	return New(ErrNotFound, message)
}

func Conflict(message string) *AppError {
	return New(ErrConflict, message)
}

func Unauthorized(message string) *AppError {
	return New(ErrUnauthorized, message)
}

func Forbidden(message string) *AppError {
	return New(ErrForbidden, message)
}

func BadRequest(message string) *AppError {
	return New(ErrBadRequest, message)
}

func ValidationError(message string) *AppError {
	return New(ErrValidation, message)
}

func InternalServerError(err error) *AppError {
	return Wrap(ErrInternalServer, "Internal server error", err)
}

func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if ok := errors.As(err, &appErr); ok {
		return appErr, true
	}
	return nil, false
}

func Is(err error, code ErrorCode) bool {
	appErr, ok := IsAppError(err)
	return ok && appErr.Code == code
}
