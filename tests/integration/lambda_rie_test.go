//go:build integration

package integration

// These tests run the real Lambda binary (./cmd built as `bootstrap`) under
// AWS's Runtime Interface Emulator, so the whole path is exercised: the
// lambda.Start loop, event decoding, dispatch, and the production router from
// cmd/app.go -- which, unlike the router in setupTestApp, nothing else in this
// suite touches. Events are sent in exactly the shapes EventBridge Scheduler
// and Function URLs deliver.
//
// The emulator is a separate download (`just lambda-rie-install`); without it
// the tests skip.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"
	stripeapi "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/webhook"
)

const rieInvokePath = "/2015-03-31/functions/function/invocations"

// lockedBuffer collects the emulator's output, which is written from another
// goroutine while the test may read it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type lambdaEmulator struct {
	invokeURL string
	client    *http.Client
	logs      *lockedBuffer
}

// invocationResult is what the emulator hands back: the handler's JSON result
// on success, or Lambda's {errorMessage, errorType} envelope when the handler
// returned an error.
type invocationResult struct {
	raw          []byte
	errorMessage string
}

func findRIE(t *testing.T) string {
	t.Helper()

	if p := os.Getenv("AWS_LAMBDA_RIE"); p != "" {
		return p
	}
	if root, err := findProjectRoot(); err == nil {
		if p := filepath.Join(root, "bin", "aws-lambda-rie"); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("aws-lambda-rie"); err == nil {
		return p
	}
	t.Skip("aws-lambda-rie not found: run `just lambda-rie-install` or set AWS_LAMBDA_RIE")
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func freePort(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

// startLambdaEmulator builds ./cmd for the host and starts it under the RIE,
// wired to the testcontainers Postgres and Redis that setupOffline started.
func startLambdaEmulator(t *testing.T) *lambdaEmulator {
	t.Helper()

	rie := findRIE(t)

	root, err := findProjectRoot()
	if err != nil {
		t.Fatalf("find project root: %v", err)
	}

	// Run from an empty directory so main's godotenv lookup can't pick up a
	// developer's .env.local and point the function at the wrong database.
	workDir := t.TempDir()
	bootstrap := filepath.Join(workDir, "bootstrap")

	build := exec.Command("go", "build", "-tags", "lambda.norpc", "-o", bootstrap, "./cmd")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build bootstrap: %v\n%s", err, out)
	}

	pgURL, err := pgContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	apiPort, runtimePort := freePort(t), freePort(t)

	cmd := exec.Command(rie,
		"--runtime-interface-emulator-address", "127.0.0.1:"+apiPort,
		"--runtime-api-address", "127.0.0.1:"+runtimePort,
		bootstrap,
	)
	cmd.Dir = workDir
	// Later entries win, so these override anything setupWith loaded from
	// .env.local into the test process.
	cmd.Env = append(os.Environ(),
		"GOOSE_DBSTRING="+pgURL,
		"REDIS_URL=redis://"+redisOpts.Addr,
		"JWT_SECRET=lambda-rie-test-secret",
		"STRIPE_SECRET_KEY=sk_test_unused",
		"STRIPE_WEBHOOK_SECRET="+offlineWebhookSecret,
		"FRONTEND_URL=http://localhost:8000",
		"AWS_REGION=us-east-1",
	)
	logs := &lockedBuffer{}
	cmd.Stdout = logs
	cmd.Stderr = logs

	if err := cmd.Start(); err != nil {
		t.Fatalf("start aws-lambda-rie: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("emulator output:\n%s", logs.String())
		}
	})

	emu := &lambdaEmulator{
		invokeURL: "http://127.0.0.1:" + apiPort + rieInvokePath,
		client:    &http.Client{Timeout: 60 * time.Second},
		logs:      logs,
	}

	// The emulator listens straight away; the function itself boots on the
	// first invoke, so only wait for the port.
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:"+apiPort, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("emulator never listened on %s:\n%s", apiPort, logs.String())
		}
		time.Sleep(100 * time.Millisecond)
	}

	return emu
}

func (e *lambdaEmulator) invoke(t *testing.T, payload any) invocationResult {
	t.Helper()

	var body []byte
	switch p := payload.(type) {
	case string:
		body = []byte(p)
	default:
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = b
	}

	resp, err := e.client.Post(e.invokeURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("invoke: %v\nemulator output:\n%s", err, e.logs.String())
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read invoke response: %v", err)
	}

	res := invocationResult{raw: raw}
	var fnErr struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(raw, &fnErr) == nil {
		res.errorMessage = fnErr.ErrorMessage
	}
	return res
}

func (e *lambdaEmulator) mustSucceed(t *testing.T, payload any) invocationResult {
	t.Helper()

	res := e.invoke(t, payload)
	if res.errorMessage != "" {
		t.Fatalf("invocation failed: %s\nemulator output:\n%s", res.errorMessage, e.logs.String())
	}
	return res
}

// functionURLEvent builds the payload a Function URL sends for one request.
func functionURLEvent(method, path string, headers map[string]string, body []byte) events.LambdaFunctionURLRequest {
	ev := events.LambdaFunctionURLRequest{
		Version:         "2.0",
		RawPath:         path,
		Headers:         headers,
		Body:            base64.StdEncoding.EncodeToString(body),
		IsBase64Encoded: true,
	}
	ev.RequestContext.HTTP.Method = method
	ev.RequestContext.HTTP.Path = path
	ev.RequestContext.HTTP.SourceIP = "198.51.100.10"
	ev.RequestContext.DomainName = "test.lambda-url.us-east-1.on.aws"
	return ev
}

func decodeURLResponse(t *testing.T, res invocationResult) (int, string) {
	t.Helper()

	var out events.LambdaFunctionURLResponse
	if err := json.Unmarshal(res.raw, &out); err != nil {
		t.Fatalf("decode function url response %q: %v", res.raw, err)
	}
	body := out.Body
	if out.IsBase64Encoded {
		b, err := base64.StdEncoding.DecodeString(out.Body)
		if err != nil {
			t.Fatalf("decode response body: %v", err)
		}
		body = string(b)
	}
	return out.StatusCode, body
}

func TestLambdaEmulator_Integration(t *testing.T) {
	findRIE(t) // skip before paying for containers

	if err := setupOffline(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance
	emu := startLambdaEmulator(t)

	t.Run("scheduled run completes due transactions", func(t *testing.T) {
		senderID := userIDFromToken(app.newUser(t, "rie-run-sender"))
		receiverID := userIDFromToken(app.newUser(t, "rie-run-receiver"))

		const funded, amount = 5000, 3000
		seedBalance(t, senderID, funded)

		due := time.Now().Add(-time.Minute)
		txID := insertTransaction(t, senderID, receiverID, amount,
			repo.TransactionStatusScheduled, due, due)
		future := time.Now().Add(time.Hour)
		laterID := insertTransaction(t, senderID, receiverID, amount,
			repo.TransactionStatusScheduled, future, future)

		emu.mustSucceed(t, `{"job":"run"}`)

		if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
			t.Errorf("due transaction status = %q, want completed", got)
		}
		if got := statusOf(t, laterID); got != string(repo.TransactionStatusScheduled) {
			t.Errorf("future transaction status = %q, want scheduled", got)
		}
		if got := balanceOf(t, senderID); got != funded-amount {
			t.Errorf("sender balance = %d, want %d", got, funded-amount)
		}
		if got := balanceOf(t, receiverID); got != amount {
			t.Errorf("receiver balance = %d, want %d", got, amount)
		}

		// EventBridge can deliver a tick twice; the second must be a no-op.
		emu.mustSucceed(t, `{"job":"run"}`)
		if got := balanceOf(t, senderID); got != funded-amount {
			t.Errorf("DOUBLE EXECUTION: sender balance = %d after repeat tick, want %d", got, funded-amount)
		}

		assertZeroSum(t)
	})

	t.Run("scheduled sweep finishes stuck crediting", func(t *testing.T) {
		userID := userIDFromToken(app.newUser(t, "rie-sweep"))

		const amount = 4000
		stale := time.Now().Add(-10 * time.Minute)
		txID := insertTransaction(t, users.SystemStripeID, userID, amount,
			repo.TransactionStatusCrediting, stale, stale)

		emu.mustSucceed(t, `{"job":"sweep"}`)

		if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
			t.Errorf("status = %q, want completed", got)
		}
		if got := balanceOf(t, userID); got != amount {
			t.Errorf("balance = %d, want %d", got, amount)
		}

		assertZeroSum(t)
	})

	t.Run("bad events fail the invocation and the function keeps serving", func(t *testing.T) {
		for name, payload := range map[string]string{
			"unknown job":   `{"job":"drop-tables"}`,
			"neither shape": `{"source":"aws.events","detail":{}}`,
		} {
			if res := emu.invoke(t, payload); res.errorMessage == "" {
				t.Errorf("%s: invocation succeeded, want an error (got %s)", name, res.raw)
			}
		}

		// A returned error must not take the process down with it.
		emu.mustSucceed(t, `{"job":"run"}`)
	})

	t.Run("function url request reaches the production router", func(t *testing.T) {
		res := emu.mustSucceed(t, functionURLEvent(http.MethodGet, "/healthx", map[string]string{}, nil))
		if status, body := decodeURLResponse(t, res); status != http.StatusNoContent {
			t.Errorf("GET /healthx = %d, want 204: %s", status, body)
		}

		res = emu.mustSucceed(t, functionURLEvent(http.MethodGet, "/no-such-route", map[string]string{}, nil))
		if status, _ := decodeURLResponse(t, res); status != http.StatusNotFound {
			t.Errorf("GET /no-such-route = %d, want 404", status)
		}
	})

	t.Run("signed stripe webhook through the function url credits once", func(t *testing.T) {
		userID := userIDFromToken(app.newUser(t, "rie-webhook"))

		const amount = 2500
		txID := pendingTopUp(t, userID, amount)

		payload := []byte(fmt.Sprintf(
			`{"id":"evt_rie_%s","object":"event","api_version":"%s","type":"payment_intent.succeeded",`+
				`"data":{"object":{"id":"pi_rie_%s","object":"payment_intent",`+
				`"metadata":{"transaction_id":"%s"}}}}`,
			uuid.New(), stripeapi.APIVersion, uuid.New(), txID,
		))
		now := time.Now()
		sig := webhook.ComputeSignature(now, payload, offlineWebhookSecret)
		headers := map[string]string{
			"content-type":     "application/json",
			"stripe-signature": fmt.Sprintf("t=%d,v1=%x", now.Unix(), sig),
		}

		// Two deliveries, as Stripe retries: both 2xx, one credit.
		for i := range 2 {
			res := emu.mustSucceed(t, functionURLEvent(http.MethodPost, "/webhook/stripe", headers, payload))
			if status, body := decodeURLResponse(t, res); status < 200 || status > 299 {
				t.Fatalf("delivery %d = %d, want 2xx: %s", i+1, status, body)
			}
		}

		if got := statusOf(t, txID); got != string(repo.TransactionStatusCompleted) {
			t.Errorf("status = %q, want completed", got)
		}
		if got := balanceOf(t, userID); got != amount {
			t.Errorf("balance = %d, want %d", got, amount)
		}

		// A body altered in transit must not verify.
		tampered := bytes.Replace(payload, []byte(`"object":"event"`), []byte(`"object": "event"`), 1)
		res := emu.mustSucceed(t, functionURLEvent(http.MethodPost, "/webhook/stripe", headers, tampered))
		if status, _ := decodeURLResponse(t, res); status < 400 {
			t.Errorf("tampered webhook = %d, want a 4xx", status)
		}

		assertZeroSum(t)
	})

	if t.Failed() {
		return
	}
	if !strings.Contains(emu.logs.String(), "Running scheduled job") {
		t.Errorf("emulator output never showed a scheduled job running")
	}
}
