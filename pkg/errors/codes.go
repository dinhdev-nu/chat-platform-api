package errors

type ErrorCode string

const (
	// Common
	ErrInternalServer     ErrorCode = "INTERNAL_SERVER_ERROR"
	ErrValidation         ErrorCode = "VALIDATION_ERROR"
	ErrInvalidRequest     ErrorCode = "INVALID_REQUEST"
	ErrUnauthorized       ErrorCode = "UNAUTHORIZED"
	ErrForbidden          ErrorCode = "FORBIDDEN"
	ErrNotFound           ErrorCode = "NOT_FOUND"
	ErrBadRequest         ErrorCode = "BAD_REQUEST"
	ErrConflict           ErrorCode = "CONFLICT"
	ErrTooManyRequests    ErrorCode = "TOO_MANY_REQUESTS"
	ErrInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"

	// Custom
	ErrTokenMissing ErrorCode = "TOKEN_MISSING"
	ErrTokenInvalid ErrorCode = "TOKEN_INVALID"
	ErrUserNotFound ErrorCode = "USER_NOT_FOUND"
)

var httpStatusMap = map[ErrorCode]int{
	ErrInternalServer:     500,
	ErrValidation:         400,
	ErrInvalidRequest:     400,
	ErrUnauthorized:       401,
	ErrForbidden:          403,
	ErrNotFound:           404,
	ErrBadRequest:         400,
	ErrConflict:           409,
	ErrTooManyRequests:    429,
	ErrInvalidCredentials: 400,
	ErrTokenMissing:       401,
	ErrTokenInvalid:       401,
	ErrUserNotFound:       404,
}

func (c ErrorCode) HttpStatus() int {
	if status, exists := httpStatusMap[c]; exists {
		return status
	}
	return 500 // Default to Internal Server Error
}
