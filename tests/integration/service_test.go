//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
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
	stripeCLIStdout *os.File
	stripeCLIStderr *os.File
	redisContainer  *rediscontainer.RedisContainer
	pgContainer     *postgres.PostgresContainer
	pgPool          *pgxpool.Pool
	redisClient     *goredis.Client
	database        *db.DB
	redisOpts       *goredis.Options
	testAppInstance *testApp
)

type testApp struct {
	server     *http.Server
	addr       string
	httpClient *http.Client
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func checkStripeCLI() error {
	cmd := exec.Command("stripe", "listen", "--help")
	if err := cmd.Run(); err != nil {
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

func startStripeCLI() error {
	var err error
	stripeCLIStdout, err = os.CreateTemp("", "stripe-cli-stdout-*.log")
	if err != nil {
		return fmt.Errorf("create stdout temp file: %w", err)
	}
	stripeCLIStderr, err = os.CreateTemp("", "stripe-cli-stderr-*.log")
	if err != nil {
		stripeCLIStdout.Close()
		return fmt.Errorf("create stderr temp file: %w", err)
	}

	forwardURL := fmt.Sprintf("http://127.0.0.1:%s/webhook/stripe", serverPort)
	slog.Info("Starting Stripe CLI forwarding to", "url", forwardURL)

	cmd := exec.Command(
		"stripe", "listen",
		"--forward-to", forwardURL,
		"--print-secret",
	)
	cmd.Stdout = stripeCLIStdout
	cmd.Stderr = stripeCLIStderr

	if err := cmd.Start(); err != nil {
		stripeCLIStdout.Close()
		stripeCLIStderr.Close()
		return fmt.Errorf("start stripe listen: %w", err)
	}
	stripeCLICmd = cmd

	time.Sleep(3 * time.Second)

	stripeCLIStdout.Seek(0, 0)
	buf := make([]byte, 2048)
	n, _ := stripeCLIStdout.Read(buf)
	output := string(buf[:n])

	slog.Info("Stripe CLI output", "output", output)

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "whsec_") {
			webhookSecret = strings.TrimSpace(line)
			break
		}
	}

	if webhookSecret == "" {
		webhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	}

	if webhookSecret == "" {
		stopStripeCLI()
		return fmt.Errorf("failed to get webhook secret from stripe cli - ensure 'stripe login' was run")
	}

	os.Setenv("STRIPE_WEBHOOK_SECRET", webhookSecret)
	slog.Info("Stripe webhook secret configured", "prefix", webhookSecret[:min(10, len(webhookSecret))]+"...")

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func stopStripeCLI() {
	if stripeCLICmd != nil && stripeCLICmd.Process != nil {
		stripeCLICmd.Process.Kill()
		stripeCLICmd.Wait()
	}
	if stripeCLIStdout != nil {
		stripeCLIStdout.Close()
		os.Remove(stripeCLIStdout.Name())
	}
	if stripeCLIStderr != nil {
		stripeCLIStderr.Close()
		os.Remove(stripeCLIStderr.Name())
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
		parent := dir + "/.."
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

	migrationSQL, err := os.ReadFile(projectRoot + "/internal/db/postgresql/migrations/00001_init.sql")
	if err != nil {
		return fmt.Errorf("read migration file: %w", err)
	}

	statements := parseGooseStatements(string(migrationSQL))

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		_, err := pgPool.Exec(ctx, stmt)
		if err != nil {
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

func setup() error {
	mu.Lock()

	if projectRoot, err := findProjectRoot(); err == nil {
		godotenv.Load(projectRoot + "/.env")
	}

	os.Setenv("STRIPE_WEBHOOK_SKIP_SIGNATURE", "true")

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
		return fmt.Errorf("run migrations: %w", err)
	}

	app, err := setupTestApp()
	if err != nil {
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("setup test app: %w", err)
	}
	slog.Info("HTTP server started, now starting Stripe CLI...")

	if err := startStripeCLI(); err != nil {
		app.close()
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("start stripe cli: %w", err)
	}

	return nil
}

func teardown() {
	ctx := context.Background()
	cleanup(ctx)
	mu.Unlock()
}

func setupValidation() error {
	mu.Lock()

	if projectRoot, err := findProjectRoot(); err == nil {
		godotenv.Load(projectRoot + "/.env")
	}

	os.Setenv("STRIPE_WEBHOOK_SKIP_SIGNATURE", "true")

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

	app, err := setupTestApp()
	if err != nil {
		cleanup(ctx)
		mu.Unlock()
		return fmt.Errorf("setup test app: %w", err)
	}
	_ = app

	if err := startStripeCLI(); err != nil {
		slog.Warn("Stripe CLI not started, webhook tests may fail", "error", err)
	} else {
		os.Setenv("STRIPE_WEBHOOK_SECRET", webhookSecret)
	}

	return nil
}

func teardownValidation() {
	ctx := context.Background()
	if testAppInstance != nil {
		testAppInstance.close()
		testAppInstance = nil
	}
	if pgPool != nil {
		pgPool.Close()
	}
	if redisClient != nil {
		redisClient.Close()
	}

	cleanupContainers(ctx)
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
	authHandler := auth.NewHandler(authService)

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
			r.Post("/register", api.Wrap(authHandler.RegisterController))
			r.Post("/login", api.Wrap(authHandler.LoginController))
		})
	})

	r.Group(func(rprotected chi.Router) {
		rprotected.Use(jwtauth.Verifier(authenticator.TokenAuth))
		rprotected.Use(authenticator.AuthHandler())

		rprotected.Route("/wallet", func(r chi.Router) {
			r.Get("/", api.Wrap(walletHandler.GetByUserId))
			r.Patch("/", api.Wrap(walletHandler.TopUp))
		})

		rprotected.Route("/transfers", func(r chi.Router) {
			r.Get("/", api.Wrap(transfersHandler.GetTransfers))
			r.Post("/", api.Wrap(transfersHandler.CreateTransfer))
		})
	})

	r.Route("/webhook", func(r chi.Router) {
		r.Post("/stripe", api.Wrap(webhookHandler.Handle))
		r.Get("/debug", func(w http.ResponseWriter, r *http.Request) {
			slog.Info("DEBUG: webhook endpoint hit!")
			w.Write([]byte("ok"))
		})
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

func (app *testApp) registerUser(t *testing.T, email, password string) (string, string) {
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
		body, _ := json.Marshal(map[string]interface{}{})
		resp.Body.Read(body)
		t.Fatalf("register failed with status %d, body: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	jwtToken := result["jwt_token"].(string)

	return jwtToken, ""
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

func (app *testApp) getWallet(t *testing.T, token string) map[string]interface{} {
	req, err := http.NewRequest("GET", app.addr+"/wallet/", nil)
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

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	return result, resp.StatusCode
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
	wallet := app.getWallet(t, token)
	if balance, ok := wallet["balance"].(float64); ok {
		return int64(balance)
	}
	return 0
}

func waitForWebhook(processingTimeout time.Duration, checkFn func() bool) error {
	deadline := time.Now().Add(processingTimeout)
	for time.Now().Before(deadline) {
		if checkFn() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for webhook")
}

func TestTopUp_HappyPath_Integration(t *testing.T) {
	slog.Info("TEST: Starting setup...")
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	slog.Info("TEST: Setup complete, testAppInstance=", "nil", testAppInstance == nil)
	defer teardown()

	app := testAppInstance
	if app == nil {
		t.Fatal("test app not initialized")
	}

	slog.Info("TEST: Using app at", "addr", app.addr)
	webhookURL := app.addr + "/webhook/stripe"
	slog.Info("Testing webhook connectivity", "url", webhookURL)

	testResp, err := app.httpClient.Post(app.addr+"/webhook/debug", "text/plain", nil)
	if err != nil {
		t.Logf("DEBUG endpoint error: %v", err)
	} else {
		t.Logf("DEBUG endpoint status: %d", testResp.StatusCode)
	}

	resp, err := app.httpClient.Get(app.addr + "/health")
	if err != nil {
		t.Logf("Health check failed: %v", err)
	} else {
		t.Logf("Health check status: %d", resp.StatusCode)
	}

	userEmail := fmt.Sprintf("user-%s@example.com", uuid.New().String())
	accessToken, _ := app.registerUser(t, userEmail, testUserPassword)

	initialWallet := app.getWallet(t, accessToken)
	initialBalance := int64(initialWallet["balance"].(float64))

	topUpAmount := int64(50000)
	result, statusCode, body := app.topUpWithBody(t, accessToken, topUpAmount)

	if statusCode != http.StatusOK {
		t.Fatalf("topup failed with status %d, response: %s", statusCode, string(body))
	}

	paymentID := result["provider_payment_id"].(string)
	slog.Info("Payment created", "payment_id", paymentID)

	err = app.confirmPayment(paymentID)
	if err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}
	slog.Info("Payment confirmed, waiting for webhook...")

	err = waitForWebhook(15*time.Second, func() bool {
		currentBalance := app.getWalletBalance(t, accessToken)
		slog.Info("Checking balance", "current", currentBalance, "expected", initialBalance+topUpAmount)
		return currentBalance == initialBalance+topUpAmount
	})

	if err != nil {
		t.Fatalf("did not receive payment_succeeded webhook: %v", err)
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
	accessToken, _ := app.registerUser(t, userEmail, testUserPassword)

	initialWallet := app.getWallet(t, accessToken)
	initialBalance := int64(initialWallet["balance"].(float64))

	reqBody := map[string]interface{}{
		"amount":         2000,
		"payment_method": "pm_card_visa_chargeDeclined",
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

	if resp.StatusCode == http.StatusOK {
		err = waitForWebhook(10*time.Second, func() bool {
			wallet := app.getWallet(t, accessToken)
			balance := int64(wallet["balance"].(float64))
			return balance == initialBalance
		})
		if err != nil {
			t.Logf("webhook timeout (payment may have failed immediately): %v", err)
		}
	}

	time.Sleep(1 * time.Second)
	finalBalance := app.getWalletBalance(t, accessToken)
	if finalBalance != initialBalance {
		t.Errorf("balance should remain %d after failed payment, got %d", initialBalance, finalBalance)
	}
}

func TestTopUp_Validation_Integration(t *testing.T) {
	if err := setupValidation(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardownValidation()

	app := testAppInstance

	userEmail := fmt.Sprintf("user-%s@example.com", uuid.New().String())
	accessToken, _ := app.registerUser(t, userEmail, testUserPassword)

	reqBody := map[string]interface{}{
		"amount": 500,
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
		t.Errorf("expected status 400 for amount below minimum, got %d", resp.StatusCode)
	}
}

func TestConcurrentTransfers_Integration(t *testing.T) {
	if err := setupValidation(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardownValidation()

	app := testAppInstance

	senderEmail := fmt.Sprintf("sender-%s@example.com", uuid.New().String())
	receiver1Email := fmt.Sprintf("receiver1-%s@example.com", uuid.New().String())
	receiver2Email := fmt.Sprintf("receiver2-%s@example.com", uuid.New().String())

	senderToken, _ := app.registerUser(t, senderEmail, testUserPassword)
	receiver1Token, _ := app.registerUser(t, receiver1Email, testUserPassword)
	receiver2Token, _ := app.registerUser(t, receiver2Email, testUserPassword)

	_, senderStatus, senderBodyStr := app.topUpWithBody(t, senderToken, 20000)
	_, r1Status, r1BodyStr := app.topUpWithBody(t, receiver1Token, 5000)
	_, r2Status, r2BodyStr := app.topUpWithBody(t, receiver2Token, 5000)

	t.Logf("Topup results - Sender: status=%d, Receiver1: status=%d, Receiver2: status=%d", senderStatus, r1Status, r2Status)

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

	if senderPaymentID == "" || r1PaymentID == "" || r2PaymentID == "" {
		t.Fatalf("failed to get payment IDs from response")
	}

	if err := app.confirmPayment(senderPaymentID); err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}
	if err := app.confirmPayment(r1PaymentID); err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}
	if err := app.confirmPayment(r2PaymentID); err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}

	for i := 0; i < 10; i++ {
		senderWallet := app.getWallet(t, senderToken)
		receiver1Wallet := app.getWallet(t, receiver1Token)
		receiver2Wallet := app.getWallet(t, receiver2Token)
		senderBalance := int64(senderWallet["balance"].(float64))
		receiver1Balance := int64(receiver1Wallet["balance"].(float64))
		receiver2Balance := int64(receiver2Wallet["balance"].(float64))
		if senderBalance >= 20000 && receiver1Balance >= 5000 && receiver2Balance >= 5000 {
			break
		}
		t.Logf("Waiting for topups... Sender: %d, R1: %d, R2: %d", senderBalance, receiver1Balance, receiver2Balance)
		time.Sleep(1 * time.Second)
	}

	senderWallet := app.getWallet(t, senderToken)
	receiver1Wallet := app.getWallet(t, receiver1Token)
	receiver2Wallet := app.getWallet(t, receiver2Token)

	receiver1WalletID := receiver1Wallet["id"].(string)
	receiver2WalletID := receiver2Wallet["id"].(string)

	t.Logf("Initial - Sender: %v, Receiver1: %v, Receiver2: %v",
		senderWallet["balance"], receiver1Wallet["balance"], receiver2Wallet["balance"])

	var wg sync.WaitGroup
	errChan := make(chan error, 4)
	resultChan := make(chan map[string]interface{}, 4)

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
			errChan <- err
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resultChan <- result
		t.Logf("Transfer to receiver1 response: status=%d, result=%v", resp.StatusCode, result)
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
			errChan <- err
			return
		}
		defer resp.Body.Close()

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		resultChan <- result
		t.Logf("Transfer to receiver2 response: status=%d, result=%v", resp.StatusCode, result)
	}()

	wg.Wait()
	close(errChan)
	close(resultChan)

	time.Sleep(500 * time.Millisecond)

	finalSenderWallet := app.getWallet(t, senderToken)
	finalReceiver1Wallet := app.getWallet(t, receiver1Token)
	finalReceiver2Wallet := app.getWallet(t, receiver2Token)

	senderBalance := int64(finalSenderWallet["balance"].(float64))
	receiver1Balance := int64(finalReceiver1Wallet["balance"].(float64))
	receiver2Balance := int64(finalReceiver2Wallet["balance"].(float64))

	t.Logf("Final - Sender: %d, Receiver1: %d, Receiver2: %d",
		senderBalance, receiver1Balance, receiver2Balance)

	totalOut := (20000 - senderBalance)
	totalIn := (receiver1Balance - 5000) + (receiver2Balance - 5000)

	t.Logf("Total deducted from sender: %d, Total credited to receivers: %d", totalOut, totalIn)

	if totalOut != totalIn {
		t.Errorf("ATOMICITY BUG: Money in (%d) != Money out (%d) - funds lost or created!", totalIn, totalOut)
	}

	if senderBalance < 0 {
		t.Errorf("ATOMICITY BUG: Sender balance is negative: %d", senderBalance)
	}
}

func TestConcurrentTransfers_ExceedBalance_Integration(t *testing.T) {
	if err := setupValidation(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardownValidation()

	app := testAppInstance

	senderEmail := fmt.Sprintf("sender2-%s@example.com", uuid.New().String())
	receiverEmail := fmt.Sprintf("receiver-%s@example.com", uuid.New().String())

	senderToken, _ := app.registerUser(t, senderEmail, testUserPassword)
	receiverToken, _ := app.registerUser(t, receiverEmail, testUserPassword)

	app.topUp(t, senderToken, 5000)
	app.topUp(t, receiverToken, 1000)

	senderWallet := app.getWallet(t, senderToken)
	receiverWallet := app.getWallet(t, receiverToken)

	_ = senderWallet["id"].(string)

	t.Logf("Initial - Sender: %v, Receiver: %v",
		senderWallet["balance"], receiverWallet["balance"])

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]interface{}{
			"to_wallet_id":       receiverWallet["id"].(string),
			"amount_in_piastres": 3000,
		})
		req, _ := http.NewRequest("POST", app.addr+"/transfers/", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+senderToken)
		req.Header.Set("Content-Type", "application/json")

		resp, _ := app.httpClient.Do(req)
		if resp != nil {
			defer resp.Body.Close()
			t.Logf("Transfer 1 status: %d", resp.StatusCode)
		}
	}()

	go func() {
		defer wg.Done()
		body, _ := json.Marshal(map[string]interface{}{
			"to_wallet_id":       receiverWallet["id"].(string),
			"amount_in_piastres": 3000,
		})
		req, _ := http.NewRequest("POST", app.addr+"/transfers/", bytes.NewBuffer(body))
		req.Header.Set("Authorization", "Bearer "+senderToken)
		req.Header.Set("Content-Type", "application/json")

		resp, _ := app.httpClient.Do(req)
		if resp != nil {
			defer resp.Body.Close()
			t.Logf("Transfer 2 status: %d", resp.StatusCode)
		}
	}()

	wg.Wait()

	time.Sleep(500 * time.Millisecond)

	finalSenderWallet := app.getWallet(t, senderToken)
	finalReceiverWallet := app.getWallet(t, receiverToken)

	senderBalance := int64(finalSenderWallet["balance"].(float64))
	receiverBalance := int64(finalReceiverWallet["balance"].(float64))

	t.Logf("Final - Sender: %d, Receiver: %d", senderBalance, receiverBalance)

	if senderBalance < 0 {
		t.Errorf("RACE CONDITION: Sender balance went negative (%d) - double spending detected!", senderBalance)
	}

	if receiverBalance > 7000 {
		t.Errorf("RACE CONDITION: Receiver got too much money (%d) - expected max 7000", receiverBalance)
	}
}
