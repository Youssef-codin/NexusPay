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
	serverPort       = "3001"
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

	cmd := exec.Command(
		"stripe", "listen",
		"--forward-to", fmt.Sprintf("localhost:%s/webhook/stripe", serverPort),
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

	if err := checkStripeCLI(); err != nil {
		mu.Unlock()
		return err
	}

	if err := checkStripeKey(); err != nil {
		mu.Unlock()
		return err
	}

	if err := startStripeCLI(); err != nil {
		mu.Unlock()
		return fmt.Errorf("start stripe cli: %w", err)
	}

	ctx := context.Background()
	if err := startTestcontainers(ctx); err != nil {
		stopStripeCLI()
		mu.Unlock()
		return fmt.Errorf("start testcontainers: %w", err)
	}

	if err := runMigrations(ctx); err != nil {
		cleanup(ctx)
		return fmt.Errorf("run migrations: %w", err)
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

	return nil
}

func teardownValidation() {
	ctx := context.Background()
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

	authRepo := auth.NewAuthRepo(database)

	transactionRepo := transactions.NewTransactionRepo(database)
	transactionService := transactions.NewService(transactionRepo)

	stripeAPIKey := os.Getenv("STRIPE_SECRET_KEY")
	paymentService := stripe.NewService(stripeAPIKey)

	walletRepo := wallet.NewWalletRepo(database)
	walletService := wallet.NewService(database, walletRepo, transactionService, paymentService)
	walletHandler := wallet.NewHandler(walletService)

	authService := auth.NewService(database, authRepo, userCache, authenticator, walletService)
	authHandler := auth.NewHandler(authService)

	userRepo := users.NewUserRepo(database)
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

	return &testApp{
		server:     server,
		addr:       "http://localhost:" + serverPort,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
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
	if err := setup(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	defer teardown()

	app, err := setupTestApp()
	if err != nil {
		t.Fatalf("setup test app: %v", err)
	}
	defer app.close()

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

	err = app.confirmPayment(paymentID)
	if err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}

	err = waitForWebhook(10*time.Second, func() bool {
		currentBalance := app.getWalletBalance(t, accessToken)
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

	app, err := setupTestApp()
	if err != nil {
		t.Fatalf("setup test app: %v", err)
	}
	defer app.close()

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

	app, err := setupTestApp()
	if err != nil {
		t.Fatalf("setup test app: %v", err)
	}
	defer app.close()

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
