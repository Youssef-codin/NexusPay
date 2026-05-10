//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/auth"
	"github.com/Youssef-codin/NexusPay/internal/db"
	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/Youssef-codin/NexusPay/internal/payment/stripe"
	"github.com/Youssef-codin/NexusPay/internal/security"
	"github.com/Youssef-codin/NexusPay/internal/transactions"
	"github.com/Youssef-codin/NexusPay/internal/transfers"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/Youssef-codin/NexusPay/internal/wallet"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/go-chi/jwtauth/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	goredis "github.com/redis/go-redis/v9"
	stripeapi "github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/paymentintent"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

const (
	serverPort       = "3002"
	testUserEmail    = "integration-test@example.com"
	testUserPassword = "TestPassword123!"
)

var (
	mu              sync.Mutex
	webhookSecret   string
	stripeCLICmd    *exec.Cmd
	redisContainer  *rediscontainer.RedisContainer
	pgContainer     *postgres.PostgresContainer
	pgPool          *pgxpool.Pool
	redisClient     *goredis.Client
	database        *db.DB
	redisOpts       *goredis.Options
	testAppInstance *testApp
)

// ansiEscape strips ANSI terminal color/control codes from strings.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

type testApp struct {
	server     *http.Server
	addr       string
	httpClient *http.Client
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func checkStripeCLI() error {
	if err := exec.Command("stripe", "listen", "--help").Run(); err != nil {
		return fmt.Errorf("stripe CLI required: run 'stripe login' first")
	}
	return nil
}

func checkStripeKey() error {
	if os.Getenv("STRIPE_SECRET_KEY") == "" {
		return fmt.Errorf("STRIPE_SECRET_KEY environment variable not set")
	}
	return nil
}

// startStripeCLI starts `stripe listen` and captures the webhook signing secret
// from its output using pipes. The secret is ready before this function returns.
func startStripeCLI() error {
	forwardURL := fmt.Sprintf("http://127.0.0.1:%s/webhook/stripe", serverPort)
	slog.Info("Starting Stripe CLI", "url", forwardURL)

	cmd := exec.Command("stripe", "listen",
		"--forward-to", forwardURL,
		"--print-secret",
	)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start stripe listen: %w", err)
	}
	stripeCLICmd = cmd

	secretCh := make(chan string, 1)

	scanForSecret := func(r io.Reader) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := ansiEscape.ReplaceAllString(strings.TrimSpace(scanner.Text()), "")
			// With --print-secret the secret appears as its own line.
			// Some versions embed it in a longer line: "signing secret is whsec_xxx"
			if idx := strings.Index(line, "whsec_"); idx >= 0 {
				secret := strings.Fields(line[idx:])[0]
				select {
				case secretCh <- secret:
				default:
				}
			}
		}
	}

	go scanForSecret(stdoutPipe)
	go scanForSecret(stderrPipe)

	select {
	case secret := <-secretCh:
		webhookSecret = secret
		slog.Info("Stripe webhook secret captured")
		return nil
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		stripeCLICmd = nil
		return fmt.Errorf("timed out waiting for webhook secret — ensure 'stripe login' was run")
	}
}

func stopStripeCLI() {
	if stripeCLICmd != nil && stripeCLICmd.Process != nil {
		stripeCLICmd.Process.Kill()
		stripeCLICmd.Wait()
		stripeCLICmd = nil
	}
}

func startTestcontainers(ctx context.Context) error {
	var err error

	pgContainer, err = postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("nexuspay"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	if err != nil {
		return fmt.Errorf("start postgres: %w", err)
	}

	redisContainer, err = rediscontainer.Run(ctx,
		"redis:7-alpine",
	)
	if err != nil {
		pgContainer.Terminate(ctx)
		return fmt.Errorf("start redis: %w", err)
	}

	pgURL, err := pgContainer.ConnectionString(ctx)
	if err != nil {
		return fmt.Errorf("get postgres connection string: %w", err)
	}

	pgPool, err = pgxpool.New(ctx, pgURL)
	if err != nil {
		return fmt.Errorf("create pg pool: %w", err)
	}

	for i := 0; i < 30; i++ {
		if err := pgPool.Ping(ctx); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping pg pool: %w", err)
	}

	redisHost, _ := redisContainer.Host(ctx)
	redisPort, _ := redisContainer.MappedPort(ctx, "6379")
	redisClient = goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", redisHost, redisPort.Port()),
	})

	database = db.New(pgPool)
	redisOpts = &goredis.Options{Addr: fmt.Sprintf("%s:%s", redisHost, redisPort.Port())}

	return nil
}

func cleanupContainers(ctx context.Context) {
	if pgContainer != nil {
		pgContainer.Terminate(ctx)
	}
	if redisContainer != nil {
		redisContainer.Terminate(ctx)
	}
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

func runMigrations(ctx context.Context) error {
	projectRoot, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	content, err := os.ReadFile(filepath.Join(projectRoot, "internal", "db", "postgresql", "migrations", "00001_init.sql"))
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}

	for _, stmt := range parseGooseStatements(string(content)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := pgPool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("execute statement: %w", err)
		}
	}

	return nil
}

func parseGooseStatements(content string) []string {
	var statements []string
	var current strings.Builder
	inUpBlock := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "-- +goose Up") {
			inUpBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "-- +goose Down") {
			inUpBlock = false
			current.Reset()
			continue
		}
		if strings.Contains(trimmed, "-- +goose StatementBegin") {
			current.Reset()
			continue
		}
		if strings.Contains(trimmed, "-- +goose StatementEnd") {
			if inUpBlock && current.Len() > 0 {
				statements = append(statements, current.String())
			}
			current.Reset()
			continue
		}

		if inUpBlock {
			if current.Len() > 0 {
				current.WriteString("\n")
			}
			current.WriteString(trimmed)
		}
	}

	return statements
}

func cleanup(ctx context.Context) {
	if pgPool != nil {
		pgPool.Close()
	}
	if redisClient != nil {
		redisClient.Close()
	}
	cleanupContainers(ctx)
	stopStripeCLI()
}

// setup starts all dependencies, the Stripe CLI (to get the real webhook secret),
// and then the test HTTP server — in that order so the handler is created with
// the actual signing secret.
func setup() error {
	mu.Lock()

	if projectRoot, err := findProjectRoot(); err == nil {
		godotenv.Load(projectRoot + "/.env")
	}

	if err := checkStripeCLI(); err != nil {
		mu.Unlock()
		return err
	}
	if err := checkStripeKey(); err != nil {
		mu.Unlock()
		return err
	}

	ctx := context.Background()
	if err := startTestcontainers(ctx); err != nil {
		mu.Unlock()
		return fmt.Errorf("start testcontainers: %w", err)
	}

	if err := runMigrations(ctx); err != nil {
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("run migrations: %w", err)
	}

	// Start the Stripe CLI before the app so we have the signing secret ready
	// when we construct the webhook handler.
	if err := startStripeCLI(); err != nil {
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("start stripe cli: %w", err)
	}

	if _, err := setupTestApp(); err != nil {
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("setup test app: %w", err)
	}

	return nil
}

func teardown() {
	ctx := context.Background()
	cleanup(ctx)
	mu.Unlock()
}

func setupTestApp() (*testApp, error) {
	host, portStr := parseRedisAddr()
	port := parsePort(portStr)

	authenticator := security.NewAuthenticator("test-secret-key-for-integration-tests", 7*24*time.Hour)

	userCache := redisDb.NewUsers(redisClient)

	authRepo := auth.NewRepo(database)

	transactionRepo := transactions.NewRepo(database)
	transactionService := transactions.NewService(transactionRepo)

	stripeAPIKey := os.Getenv("STRIPE_SECRET_KEY")
	paymentService := stripe.NewService(stripeAPIKey)

	walletRepo := wallet.NewRepo(database)
	walletService := wallet.NewService(database, walletRepo, transactionService, paymentService)
	walletHandler := wallet.NewHandler(walletService)

	transfersRepo := transfers.NewRepo(database)
	transfersService := transfers.NewService(database, transfersRepo, walletService, transactionService)
	transfersHandler := transfers.NewHandler(transfersService)

	authService := auth.NewService(database, authRepo, userCache, authenticator, walletService)
	authHandler := auth.NewHandler(authService, authenticator)

	userRepo := users.NewRepo(database)
	userService := users.NewService(userRepo, userCache)
	_ = users.NewHandler(userService)

	webhookService := stripe.NewWebhookService(database, walletService, transactionService)
	webhookHandler := stripe.NewWebhookHandler(webhookSecret, webhookService)

	r := chi.NewRouter()

	r.Group(func(rpublic chi.Router) {
		rpublic.Use(httprate.Limit(
			100,
			time.Minute,
			httprate.WithKeyByIP(),
			httprateredis.WithRedisLimitCounter(&httprateredis.Config{
				Host: host,
				Port: uint16(port),
			}),
		))

		rpublic.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})

		rpublic.Route("/auth", func(r chi.Router) {
			r.Post("/register", api.Wrap(authHandler.RegisterHandler))
			r.Post("/login", api.Wrap(authHandler.LoginHandler))
		})
	})

	r.Group(func(rprotected chi.Router) {
		rprotected.Use(jwtauth.Verifier(authenticator.TokenAuth))
		rprotected.Use(authenticator.AuthHandler())

		rprotected.Route("/wallet", func(r chi.Router) {
			r.Get("/{userId}", api.Wrap(walletHandler.GetByUserId))
			r.Patch("/", api.Wrap(walletHandler.TopUp))
		})

		rprotected.Route("/transfers", func(r chi.Router) {
			r.Get("/", api.Wrap(transfersHandler.GetTransfers))
			r.Post("/", api.Wrap(transfersHandler.CreateTransfer))
		})
	})

	r.Route("/webhook", func(r chi.Router) {
		r.Post("/stripe", api.Wrap(webhookHandler.Handle))
	})

	server := &http.Server{
		Addr:    ":" + serverPort,
		Handler: r,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	testAppInstance = &testApp{
		server:     server,
		addr:       "http://localhost:" + serverPort,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	return testAppInstance, nil
}

func parseRedisAddr() (string, string) {
	if redisOpts == nil {
		return "localhost", "6379"
	}
	parts := strings.Split(redisOpts.Addr, ":")
	if len(parts) != 2 {
		return "localhost", "6379"
	}
	return parts[0], parts[1]
}

func parsePort(portStr string) uint16 {
	var p uint16
	fmt.Sscanf(portStr, "%d", &p)
	if p == 0 {
		p = 6379
	}
	return p
}

func (app *testApp) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app.server.Shutdown(ctx)
}

func (app *testApp) registerUser(t *testing.T, email, password string) string {
	reqBody := map[string]string{
		"email":     email,
		"password":  password,
		"full_name": "Test User",
	}
	body, _ := json.Marshal(reqBody)

	resp, err := app.httpClient.Post(app.addr+"/auth/register", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		rawBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("register failed with status %d, body: %s", resp.StatusCode, rawBody)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	return result["jwt_token"].(string)
}

func (app *testApp) loginUser(t *testing.T, email, password string) string {
	reqBody := map[string]string{
		"email":    email,
		"password": password,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := app.httpClient.Post(app.addr+"/auth/login", "application/json", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	return result["jwt_token"].(string)
}

func (app *testApp) confirmPayment(paymentIntentID string) error {
	stripeapi.Key = os.Getenv("STRIPE_SECRET_KEY")

	params := &stripeapi.PaymentIntentConfirmParams{
		PaymentMethod: stripeapi.String("pm_card_visa"),
	}
	_, err := paymentintent.Confirm(paymentIntentID, params)
	return err
}

func userIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	json.Unmarshal(payload, &claims)
	sub, _ := claims["sub"].(string)
	return sub
}

func (app *testApp) getWallet(t *testing.T, token string) map[string]interface{} {
	userID := userIDFromToken(token)
	req, err := http.NewRequest("GET", app.addr+"/wallet/"+userID, nil)
	if err != nil {
		t.Fatalf("get wallet request failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := app.httpClient.Do(req)
	if err != nil {
		t.Fatalf("get wallet request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	return result
}

func (app *testApp) topUp(t *testing.T, token string, amount int64) (map[string]interface{}, int) {
	result, status, _ := app.topUpWithBody(t, token, amount)
	return result, status
}

func (app *testApp) topUpWithBody(t *testing.T, token string, amount int64) (map[string]interface{}, int, string) {
	reqBody := map[string]interface{}{
		"amount_in_piastres": amount,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("PATCH", app.addr+"/wallet/", bytes.NewBuffer(body))
	if err != nil {
		t.Fatalf("topup request failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.httpClient.Do(req)
	if err != nil {
		t.Fatalf("topup request failed: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(bodyBytes, &result)

	return result, resp.StatusCode, string(bodyBytes)
}

func (app *testApp) getWalletBalance(t *testing.T, token string) int64 {
	w := app.getWallet(t, token)
	if balance, ok := w["balance"].(float64); ok {
		return int64(balance)
	}
	return 0
}

func waitForWebhook(timeout time.Duration, checkFn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if checkFn() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for webhook")
}

func TestTopUp_HappyPath_Integration(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	resp, err := app.httpClient.Get(app.addr + "/health")
	if err != nil {
		t.Logf("Health check failed: %v", err)
	} else {
		t.Logf("Health check status: %d", resp.StatusCode)
	}

	userEmail := fmt.Sprintf("user-%s@example.com", uuid.New().String())
	accessToken := app.registerUser(t, userEmail, testUserPassword)

	initialBalance := app.getWalletBalance(t, accessToken)

	topUpAmount := int64(50000)
	result, statusCode, body := app.topUpWithBody(t, accessToken, topUpAmount)
	if statusCode != http.StatusOK {
		t.Fatalf("topup failed with status %d, response: %s", statusCode, body)
	}

	paymentID := result["provider_payment_id"].(string)
	slog.Info("Payment created", "payment_id", paymentID)

	if err := app.confirmPayment(paymentID); err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}
	slog.Info("Payment confirmed, waiting for webhook...")

	err = waitForWebhook(15*time.Second, func() bool {
		current := app.getWalletBalance(t, accessToken)
		slog.Info("Checking balance", "current", current, "expected", initialBalance+topUpAmount)
		return current == initialBalance+topUpAmount
	})
	if err != nil {
		t.Fatalf("did not receive payment_intent.succeeded webhook: %v", err)
	}

	finalBalance := app.getWalletBalance(t, accessToken)
	if finalBalance != initialBalance+topUpAmount {
		t.Errorf("expected balance %d, got %d", initialBalance+topUpAmount, finalBalance)
	}
}

func TestTopUp_PaymentFailed_Integration(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	userEmail := fmt.Sprintf("user-%s@example.com", uuid.New().String())
	accessToken := app.registerUser(t, userEmail, testUserPassword)

	initialBalance := app.getWalletBalance(t, accessToken)

	// pm_card_visa_chargeDeclined causes Stripe to decline immediately;
	// the top-up API creates the intent but the payment never succeeds.
	result, statusCode, _ := app.topUpWithBody(t, accessToken, 10000)
	if statusCode == http.StatusOK {
		paymentID, _ := result["provider_payment_id"].(string)
		if paymentID != "" {
			// Attempt to confirm with a declining card — Stripe will reject it.
			params := &stripeapi.PaymentIntentConfirmParams{
				PaymentMethod: stripeapi.String("pm_card_visa_chargeDeclined"),
			}
			stripeapi.Key = os.Getenv("STRIPE_SECRET_KEY")
			paymentintent.Confirm(paymentID, params)
		}

		// Wait for the payment_intent.payment_failed webhook to fire.
		waitForWebhook(10*time.Second, func() bool {
			return app.getWalletBalance(t, accessToken) == initialBalance
		})
	}

	time.Sleep(1 * time.Second)
	finalBalance := app.getWalletBalance(t, accessToken)
	if finalBalance != initialBalance {
		t.Errorf("balance should remain %d after failed payment, got %d", initialBalance, finalBalance)
	}
}

func TestTopUp_Validation_Integration(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	userEmail := fmt.Sprintf("user-%s@example.com", uuid.New().String())
	accessToken := app.registerUser(t, userEmail, testUserPassword)

	// Amount below minimum should be rejected.
	reqBody := map[string]interface{}{
		"amount_in_piastres": 500,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("PATCH", app.addr+"/wallet/", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.httpClient.Do(req)
	if err != nil {
		t.Fatalf("topup request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for amount below minimum, got %d", resp.StatusCode)
	}
}

func TestConcurrentTransfers_Integration(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderEmail := fmt.Sprintf("sender-%s@example.com", uuid.New().String())
	receiver1Email := fmt.Sprintf("receiver1-%s@example.com", uuid.New().String())
	receiver2Email := fmt.Sprintf("receiver2-%s@example.com", uuid.New().String())

	senderToken := app.registerUser(t, senderEmail, testUserPassword)
	receiver1Token := app.registerUser(t, receiver1Email, testUserPassword)
	receiver2Token := app.registerUser(t, receiver2Email, testUserPassword)

	_, senderStatus, senderBodyStr := app.topUpWithBody(t, senderToken, 20000)
	_, r1Status, r1BodyStr := app.topUpWithBody(t, receiver1Token, 5000)
	_, r2Status, r2BodyStr := app.topUpWithBody(t, receiver2Token, 5000)

	if senderStatus != 200 || r1Status != 200 || r2Status != 200 {
		t.Fatalf("topup failed: sender=%d (body: %s), receiver1=%d (body: %s), receiver2=%d (body: %s)",
			senderStatus, senderBodyStr, r1Status, r1BodyStr, r2Status, r2BodyStr)
	}

	var senderBody, r1Body, r2Body map[string]interface{}
	json.Unmarshal([]byte(senderBodyStr), &senderBody)
	json.Unmarshal([]byte(r1BodyStr), &r1Body)
	json.Unmarshal([]byte(r2BodyStr), &r2Body)

	senderPaymentID := senderBody["provider_payment_id"].(string)
	r1PaymentID := r1Body["provider_payment_id"].(string)
	r2PaymentID := r2Body["provider_payment_id"].(string)

	if err := app.confirmPayment(senderPaymentID); err != nil {
		t.Fatalf("confirm sender payment failed: %v", err)
	}
	if err := app.confirmPayment(r1PaymentID); err != nil {
		t.Fatalf("confirm receiver1 payment failed: %v", err)
	}
	if err := app.confirmPayment(r2PaymentID); err != nil {
		t.Fatalf("confirm receiver2 payment failed: %v", err)
	}

	// Wait for all three top-ups to land via webhook.
	err := waitForWebhook(30*time.Second, func() bool {
		sb := app.getWalletBalance(t, senderToken)
		r1b := app.getWalletBalance(t, receiver1Token)
		r2b := app.getWalletBalance(t, receiver2Token)
		t.Logf("Waiting for top-ups... Sender: %d, R1: %d, R2: %d", sb, r1b, r2b)
		return sb >= 20000 && r1b >= 5000 && r2b >= 5000
	})
	if err != nil {
		t.Fatalf("top-ups did not complete: %v", err)
	}

	receiver1Wallet := app.getWallet(t, receiver1Token)
	receiver2Wallet := app.getWallet(t, receiver2Token)
	receiver1WalletID := receiver1Wallet["id"].(string)
	receiver2WalletID := receiver2Wallet["id"].(string)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]interface{}{
			"to_wallet_id":       receiver1WalletID,
			"amount_in_piastres": 3000,
		})
		req, _ := http.NewRequest("POST", app.addr+"/transfers/", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+senderToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.httpClient.Do(req)
		if err != nil {
			t.Logf("Transfer to receiver1 failed: %v", err)
			return
		}
		defer resp.Body.Close()
		t.Logf("Transfer to receiver1 status: %d", resp.StatusCode)
	}()

	go func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]interface{}{
			"to_wallet_id":       receiver2WalletID,
			"amount_in_piastres": 3000,
		})
		req, _ := http.NewRequest("POST", app.addr+"/transfers/", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+senderToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.httpClient.Do(req)
		if err != nil {
			t.Logf("Transfer to receiver2 failed: %v", err)
			return
		}
		defer resp.Body.Close()
		t.Logf("Transfer to receiver2 status: %d", resp.StatusCode)
	}()

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	finalSenderBalance := app.getWalletBalance(t, senderToken)
	finalReceiver1Balance := app.getWalletBalance(t, receiver1Token)
	finalReceiver2Balance := app.getWalletBalance(t, receiver2Token)

	t.Logf("Final — Sender: %d, Receiver1: %d, Receiver2: %d",
		finalSenderBalance, finalReceiver1Balance, finalReceiver2Balance)

	totalOut := 20000 - finalSenderBalance
	totalIn := (finalReceiver1Balance - 5000) + (finalReceiver2Balance - 5000)

	if totalOut != totalIn {
		t.Errorf("ATOMICITY BUG: money out (%d) != money in (%d)", totalOut, totalIn)
	}
	if finalSenderBalance < 0 {
		t.Errorf("ATOMICITY BUG: sender balance went negative: %d", finalSenderBalance)
	}
}

func TestConcurrentTransfers_ExceedBalance_Integration(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app := testAppInstance

	senderEmail := fmt.Sprintf("sender2-%s@example.com", uuid.New().String())
	receiverEmail := fmt.Sprintf("receiver-%s@example.com", uuid.New().String())

	senderToken := app.registerUser(t, senderEmail, testUserPassword)
	receiverToken := app.registerUser(t, receiverEmail, testUserPassword)

	// Top up sender and confirm so the balance is funded.
	senderTopUp, senderStatus, _ := app.topUpWithBody(t, senderToken, 5000)
	if senderStatus != http.StatusOK {
		t.Fatalf("sender topup failed with status %d", senderStatus)
	}
	if err := app.confirmPayment(senderTopUp["provider_payment_id"].(string)); err != nil {
		t.Fatalf("confirm sender payment: %v", err)
	}
	if err := waitForWebhook(15*time.Second, func() bool {
		return app.getWalletBalance(t, senderToken) >= 5000
	}); err != nil {
		t.Fatalf("sender top-up webhook did not arrive: %v", err)
	}

	receiverWallet := app.getWallet(t, receiverToken)
	receiverWalletID := receiverWallet["id"].(string)

	initialSenderBalance := app.getWalletBalance(t, senderToken)
	initialReceiverBalance := app.getWalletBalance(t, receiverToken)
	t.Logf("Initial — Sender: %d, Receiver: %d", initialSenderBalance, initialReceiverBalance)

	// Fire two concurrent transfers of 3000 each against a 5000 balance.
	// At most one should succeed.
	var wg sync.WaitGroup
	wg.Add(2)

	doTransfer := func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]interface{}{
			"to_wallet_id":       receiverWalletID,
			"amount_in_piastres": 3000,
		})
		req, _ := http.NewRequest("POST", app.addr+"/transfers/", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+senderToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.httpClient.Do(req)
		if err != nil {
			t.Logf("Transfer request error: %v", err)
			return
		}
		defer resp.Body.Close()
		t.Logf("Transfer status: %d", resp.StatusCode)
	}

	go doTransfer()
	go doTransfer()
	wg.Wait()

	time.Sleep(500 * time.Millisecond)

	finalSenderBalance := app.getWalletBalance(t, senderToken)
	finalReceiverBalance := app.getWalletBalance(t, receiverToken)

	t.Logf("Final — Sender: %d, Receiver: %d", finalSenderBalance, finalReceiverBalance)

	if finalSenderBalance < 0 {
		t.Errorf("RACE CONDITION: sender balance went negative (%d)", finalSenderBalance)
	}

	maxExpectedReceiver := initialReceiverBalance + initialSenderBalance
	if finalReceiverBalance > maxExpectedReceiver {
		t.Errorf("RACE CONDITION: receiver got more than sender had (%d > %d)", finalReceiverBalance, maxExpectedReceiver)
	}

	totalOut := initialSenderBalance - finalSenderBalance
	totalIn := finalReceiverBalance - initialReceiverBalance
	if totalOut != totalIn {
		t.Errorf("ATOMICITY BUG: money out (%d) != money in (%d)", totalOut, totalIn)
	}
}
