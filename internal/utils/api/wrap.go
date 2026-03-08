package api

import (
	"bytes"
	"errors"
	"log"
	"net/http"
)

type ResponseBuffer struct {
	Headers  http.Header
	Buffered bytes.Buffer
	Code     int
}

func (r *ResponseBuffer) Header() http.Header {
	return r.Headers
}

func (r *ResponseBuffer) Write(b []byte) (int, error) {
	return r.Buffered.Write(b)
}

func (r *ResponseBuffer) WriteHeader(statusCode int) {
	r.Code = statusCode
}

func Wrap(f func(w http.ResponseWriter, req *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		buf := &ResponseBuffer{
			Headers: w.Header(),
			Code:    http.StatusOK,
		}

		if err := f(buf, req); err != nil {
			var coded CodedError
			if !errors.As(err, &coded) {
				coded = Errorf(http.StatusInternalServerError, "un-coded error: %w", err).(CodedError)
			}
			Error(w, coded.Err.Error(), coded.Code)
			return
		}

		w.WriteHeader(buf.Code)
		if n, err := buf.Buffered.WriteTo(w); err != nil {
			log.Printf("Writing response to user: %s (after %d bytes)", err, n)
		}
	}
}
