package api

import (
	"errors"
	"fmt"
	"net/http"
)

type CodedError struct {
	Code int
	Err  error
}

func (e CodedError) Error() string {
	return fmt.Sprintf("[%d %s] %s", e.Code, http.StatusText(e.Code), e.Err)
}

func (e CodedError) Unwrap() error {
	return e.Err
}

func Errorf(code int, format string, args ...any) error {
	return CodedError{
		Code: code,
		Err:  fmt.Errorf(format, args...),
	}
}

func ErrorCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	var coded CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	return http.StatusInternalServerError
}
