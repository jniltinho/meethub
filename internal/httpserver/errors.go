package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
)

// Error is an API error: an HTTP status, a stable machine code and an English
// message. Handlers return it (or a domain error mapped by MapError).
type Error struct {
	Status  int
	Code    string
	Message string
	Headers map[string]string // e.g. Retry-After
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// NewError builds an API error.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Common API errors.
var (
	ErrBadRequest   = NewError(http.StatusBadRequest, "bad_request", "The request is malformed.")
	ErrInvalidInput = NewError(http.StatusBadRequest, "invalid_input", "The request contains invalid fields.")
	ErrNotFound     = NewError(http.StatusNotFound, "not_found", "The requested resource does not exist.")
	ErrForbidden    = NewError(http.StatusForbidden, "forbidden", "You are not allowed to do this.")
	ErrInternal     = NewError(http.StatusInternalServerError, "internal", "An internal error occurred.")
)

// errorBody is the JSON shape of every API error.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ErrorMapper turns a domain error into an API error; it returns nil when it
// does not know the error. Domain packages' sentinels are mapped in one place
// (the server's mappers), never by string matching.
type ErrorMapper func(error) *Error

// errorHandler writes every error as JSON {"error":{"code","message"}}.
func errorHandler(logger *slog.Logger, mappers []ErrorMapper) echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		if resp, uerr := echo.UnwrapResponse(c.Response()); uerr == nil && resp.Committed {
			return
		}
		apiErr := toAPIError(err, mappers)
		if apiErr.Status >= http.StatusInternalServerError {
			logger.ErrorContext(c.Request().Context(), "request failed",
				"method", c.Request().Method, "path", c.Request().URL.Path, "error", err.Error())
		}
		for k, v := range apiErr.Headers {
			c.Response().Header().Set(k, v)
		}
		var body errorBody
		body.Error.Code, body.Error.Message = apiErr.Code, apiErr.Message
		if c.Request().Method == http.MethodHead {
			_ = c.NoContent(apiErr.Status)
			return
		}
		_ = c.JSON(apiErr.Status, body)
	}
}

func toAPIError(err error, mappers []ErrorMapper) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	for _, m := range mappers {
		if e := m(err); e != nil {
			return e
		}
	}
	var sc echo.HTTPStatusCoder
	if errors.As(err, &sc) {
		switch sc.StatusCode() {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusMethodNotAllowed:
			return NewError(http.StatusMethodNotAllowed, "method_not_allowed", "This method is not allowed here.")
		case http.StatusRequestEntityTooLarge:
			return NewError(http.StatusRequestEntityTooLarge, "payload_too_large", "The request body is too large.")
		case http.StatusUnsupportedMediaType:
			return NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "The request must be JSON.")
		case http.StatusBadRequest:
			return ErrBadRequest
		case http.StatusUnauthorized:
			return NewError(http.StatusUnauthorized, "unauthenticated", "You must log in.")
		case http.StatusForbidden:
			return ErrForbidden
		case http.StatusTooManyRequests:
			return NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests.")
		case http.StatusServiceUnavailable:
			return NewError(http.StatusServiceUnavailable, "unavailable", "The service is unavailable.")
		}
	}
	return ErrInternal
}
