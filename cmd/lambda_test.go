package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"
)

func urlEvent(method, path, body string) events.LambdaFunctionURLRequest {
	ev := events.LambdaFunctionURLRequest{
		RawPath: path,
		Headers: map[string]string{},
		Body:    body,
	}
	ev.RequestContext.HTTP.Method = method
	ev.RequestContext.HTTP.SourceIP = "203.0.113.7"
	ev.RequestContext.DomainName = "abc.lambda-url.us-east-1.on.aws"
	return ev
}

func TestServeFunctionURL_PassesRawBodyQueryAndCookies(t *testing.T) {
	const payload = `{"id":"evt_1","object":"event"}`

	var gotBody, gotQuery, gotCookie, gotSig string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotQuery = r.URL.Query().Get("limit")
		gotSig = r.Header.Get("Stripe-Signature")
		if c, err := r.Cookie("refresh"); err == nil {
			gotCookie = c.Value
		}
		http.SetCookie(w, &http.Cookie{Name: "a", Value: "1"})
		http.SetCookie(w, &http.Cookie{Name: "b", Value: "2"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	ev := urlEvent(http.MethodPost, "/webhook/stripe", base64.StdEncoding.EncodeToString([]byte(payload)))
	ev.IsBase64Encoded = true
	ev.RawQueryString = "limit=5"
	ev.Headers["stripe-signature"] = "t=1,v1=abc"
	ev.Cookies = []string{"refresh=tok", "other=x"}

	res, err := serveFunctionURL(context.Background(), h, ev)
	require.NoError(t, err)

	require.Equal(t, payload, gotBody, "webhook signature needs the exact bytes")
	require.Equal(t, "5", gotQuery)
	require.Equal(t, "t=1,v1=abc", gotSig)
	require.Equal(t, "tok", gotCookie)

	require.Equal(t, http.StatusCreated, res.StatusCode)
	require.Equal(t, "application/json", res.Headers["Content-Type"])
	require.ElementsMatch(t, []string{"a=1", "b=2"}, res.Cookies)
	require.NotContains(t, res.Headers, "Set-Cookie")
	require.True(t, res.IsBase64Encoded)
	body, err := base64.StdEncoding.DecodeString(res.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
}

func TestServeFunctionURL_ClientCannotSpoofIP(t *testing.T) {
	var seen string
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { seen = r.RemoteAddr })

	ev := urlEvent(http.MethodGet, "/", "")
	ev.Headers["x-forwarded-for"] = "1.1.1.1"
	ev.Headers["x-real-ip"] = "2.2.2.2"
	ev.Headers["true-client-ip"] = "3.3.3.3"

	_, err := serveFunctionURL(context.Background(), r, ev)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.7", seen)
}

func TestServeFunctionURL_RejectsBadBase64(t *testing.T) {
	ev := urlEvent(http.MethodPost, "/", "!!not base64!!")
	ev.IsBase64Encoded = true

	_, err := serveFunctionURL(context.Background(), http.NotFoundHandler(), ev)
	require.Error(t, err)
}

// fakeJobs records which worker entrypoint a scheduled event reached.
type fakeJobs struct {
	runs, sweeps int
	err          error
}

func (f *fakeJobs) RunOnce(context.Context) error   { f.runs++; return f.err }
func (f *fakeJobs) SweepOnce(context.Context) error { f.sweeps++; return f.err }

// mustNotServe fails the test if an event that should go to the worker is
// routed to the HTTP handler instead.
func mustNotServe(t *testing.T) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Errorf("scheduled event was routed to the HTTP handler")
	})
}

func TestLambdaHandler_ScheduledJobs(t *testing.T) {
	cases := []struct {
		name               string
		payload            string
		wantRuns, wantSwps int
	}{
		{"run", `{"job":"run"}`, 1, 0},
		{"sweep", `{"job":"sweep"}`, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs := &fakeJobs{}
			res, err := lambdaHandler(mustNotServe(t), jobs)(context.Background(), []byte(tc.payload))
			require.NoError(t, err)
			require.Nil(t, res)
			require.Equal(t, tc.wantRuns, jobs.runs)
			require.Equal(t, tc.wantSwps, jobs.sweeps)
		})
	}
}

func TestLambdaHandler_JobErrorIsReturned(t *testing.T) {
	// A returned error marks the invocation failed, which is what surfaces in
	// CloudWatch metrics and alarms.
	boom := errors.New("claim failed")
	jobs := &fakeJobs{err: boom}

	_, err := lambdaHandler(mustNotServe(t), jobs)(context.Background(), []byte(`{"job":"run"}`))
	require.ErrorIs(t, err, boom)
}

func TestLambdaHandler_RejectsUnknownAndMalformedEvents(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown job":    `{"job":"drop-tables"}`,
		"no job no http": `{"source":"aws.events","detail":{}}`,
		"empty object":   `{}`,
		"not json":       `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{}
			_, err := lambdaHandler(mustNotServe(t), jobs)(context.Background(), []byte(payload))
			require.Error(t, err)
			require.Zero(t, jobs.runs+jobs.sweeps)
		})
	}
}

func TestLambdaHandler_FunctionURLRequestGoesToRouter(t *testing.T) {
	jobs := &fakeJobs{}
	var hit string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	ev := urlEvent(http.MethodGet, "/healthx", "")
	raw, err := json.Marshal(ev)
	require.NoError(t, err)

	res, err := lambdaHandler(h, jobs)(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "GET /healthx", hit)
	require.Equal(t, http.StatusNoContent, res.(events.LambdaFunctionURLResponse).StatusCode)
	require.Zero(t, jobs.runs+jobs.sweeps)
}
