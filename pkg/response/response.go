package response

import (
	"net/http"

	"github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/gin-gonic/gin"
)

type Response[T any] struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Data    *T         `json:"data,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
	Meta    *Meta      `json:"meta,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Meta struct {
	Pagination *Pagination `json:"pagination,omitempty"`
}

type Pagination struct {
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasNext"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func OK[T any](c *gin.Context, data *T, message string) {
	c.JSON(http.StatusOK, Response[T]{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func Created[T any](c *gin.Context, data *T, message string) {
	c.JSON(http.StatusCreated, Response[T]{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func Paginated[T any](c *gin.Context, data *T, pagination *Pagination, message string) {
	c.JSON(http.StatusOK, Response[T]{
		Success: true,
		Message: message,
		Data:    data,
		Meta: &Meta{
			Pagination: pagination,
		},
	})
}

func Error(c *gin.Context, err *errors.AppError) {
	c.JSON(err.HttpStatus(), Response[any]{
		Success: false,
		Message: "Failed",
		Error: &ErrorBody{
			Code:    string(err.Code),
			Message: err.Message,
		},
	})
}
