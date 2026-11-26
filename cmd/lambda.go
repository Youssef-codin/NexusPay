package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Jobs an EventBridge schedule can ask for. The schedule's Input is
// {"job": "<name>"}; see template.yaml.
const (
	jobRun   = "run"
	jobSweep = "sweep"
)

// onLambda reports whether the process was started by the Lambda runtime.
func onLambda() bool {
	return os.Getenv("AWS_LAMBDA_RUNTIME_API") != ""
}

// lambdaEvent is the union of the two payloads this function receives. A
// Function URL request always carries requestContext; a scheduled job carries
// only "job". Nothing reachable over HTTP can produce a bare {"job": ...}
// event: the URL wraps every request, and direct Invoke requires IAM.
type lambdaEvent struct {
	Job            string          `json:"job"`
	RequestContext json.RawMessage `json:"requestContext"`
}

func (app *application) runLambda(h http.Handler) {
	lambda.Start(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var ev lambdaEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}

		if ev.Job != "" {
			return nil, app.runJob(ctx, ev.Job)
		}

		var req events.LambdaFunctionURLRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("decode function url request: %w", err)
		}
		return serveFunctionURL(ctx, h, req)
	})
}

func (app *application) runJob(ctx context.Context, job string) error {
	slog.Info("Running scheduled job", "job", job)
	switch job {
	case jobRun:
		return app.scheduler.RunOnce(ctx)
	case jobSweep:
		return app.scheduler.SweepOnce(ctx)
	default:
		return fmt.Errorf("unknown job %q", job)
	}
}

// serveFunctionURL replays a Function URL event through the ordinary
// http.Handler, so the router, middleware and handlers are identical to the
// long-running server.
func serveFunctionURL(
	ctx context.Context,
	h http.Handler,
	ev events.LambdaFunctionURLRequest,
) (events.LambdaFunctionURLResponse, error) {
	r, err := toHTTPRequest(ctx, ev)
	if err != nil {
		return events.LambdaFunctionURLResponse{}, err
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return toFunctionURLResponse(w), nil
}

func toHTTPRequest(ctx context.Context, ev events.LambdaFunctionURLRequest) (*http.Request, error) {
	// The Stripe webhook verifies its signature over the exact raw bytes, so the
	// body must be passed through untouched.
	var body []byte
	if ev.IsBase64Encoded {
		b, err := base64.StdEncoding.DecodeString(ev.Body)
		if err != nil {
			return nil, fmt.Errorf("decode base64 body: %w", err)
		}
		body = b
	} else {
		body = []byte(ev.Body)
	}

	url := ev.RawPath
	if url == "" {
		url = "/"
	}
	if ev.RawQueryString != "" {
		url += "?" + ev.RawQueryString
	}

	r, err := http.NewRequestWithContext(ctx, ev.RequestContext.HTTP.Method, url, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	r.ContentLength = int64(len(body))

	for k, v := range ev.Headers {
		r.Header.Set(k, v)
	}
	if len(ev.Cookies) > 0 {
		r.Header.Set("Cookie", strings.Join(ev.Cookies, "; "))
	}
	r.Host = ev.RequestContext.DomainName

	// The rate limiters key on the client IP via middleware.RealIP, which trusts
	// forwarding headers. Behind a Function URL the only trustworthy source is
	// the one AWS observed, so overwrite whatever the client sent.
	ip := ev.RequestContext.HTTP.SourceIP
	r.Header.Del("True-Client-IP")
	r.Header.Del("X-Real-IP")
	r.Header.Set("X-Forwarded-For", ip)
	r.RemoteAddr = net.JoinHostPort(ip, "0")

	return r, nil
}

func toFunctionURLResponse(w *httptest.ResponseRecorder) events.LambdaFunctionURLResponse {
	res := w.Result()

	headers := make(map[string]string, len(res.Header))
	for k, v := range res.Header {
		if http.CanonicalHeaderKey(k) == "Set-Cookie" {
			continue
		}
		headers[k] = strings.Join(v, ",")
	}

	return events.LambdaFunctionURLResponse{
		StatusCode:      res.StatusCode,
		Headers:         headers,
		Cookies:         res.Header.Values("Set-Cookie"),
		Body:            base64.StdEncoding.EncodeToString(w.Body.Bytes()),
		IsBase64Encoded: true,
	}
}
