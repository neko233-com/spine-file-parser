package spineparser

import "fmt"

// ErrorCode identifies a stable parser error category.
type ErrorCode string

const (
	ErrInvalidInput   ErrorCode = "INVALID_INPUT"
	ErrInvalidProject ErrorCode = "INVALID_PROJECT"
	ErrInvalidJSON    ErrorCode = "INVALID_JSON"
	ErrInvalidSkel    ErrorCode = "INVALID_SKEL"
	ErrLimitExceeded  ErrorCode = "LIMIT_EXCEEDED"
)

// ParseError is returned for invalid or unsafe Spine input.
type ParseError struct {
	Code  ErrorCode
	Msg   string
	Cause error
}

func (e *ParseError) Error() string {
	if e.Cause == nil {
		return e.Msg
	}
	return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}
