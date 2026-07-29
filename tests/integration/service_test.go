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
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
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

	// Exposed so tests can drive the workers directly instead of waiting on cron.
	txService transactions.IService
	scheduler *transactions.Scheduler
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
		godotenv.Load(projectRoot + "/.env.local")
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
	if testAppInstance != nil {
		testAppInstance.close()
		testAppInstance = nil
	}
	cleanup(ctx)
	mu.Unlock()
}

func setupTestApp() (*testApp, error) {
	host, portStr := parseRedisAddr()
	port := parsePort(portStr)

	authenticator := security.NewAuthenticator("test-secret-key-for-integration-tests", 7*24*time.Hour)

	userCache := redisDb.NewUsers(redisClient)

	stripeAPIKey := os.Getenv("STRIPE_SECRET_KEY")
	paymentService := stripe.NewService(stripeAPIKey)

	authRepo := auth.NewRepo(database)
	authService := auth.NewService(database, authRepo, userCache, authenticator)
	authHandler := auth.NewHandler(authService, authenticator)

	userRepo := users.NewRepo(database)
	userService := users.NewService(userRepo, userCache)
	userHandler := users.NewHandler(userService)

	transactionRepo := transactions.NewRepo(database)
	transactionService := transactions.NewService(
		database,
		transactionRepo,
		userService,
		paymentService,
	)
	transactionHandler := transactions.NewHandler(transactionService)

	// Deliberately not Start()ed: tests drive RunOnce/SweepOnce themselves so
	// nothing races with cron.
	scheduler := transactions.NewScheduler(transactionService, transactionRepo)

	webhookService := stripe.NewWebhookService(transactionService)
	webhookHandler := stripe.NewWebhookHandler(webhookSecret, webhookService)

	r := chi.NewRouter()

	r.Group(func(rpublic chi.Router) {
		rpublic.Use(httprate.Limit(
			1000,
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

		rprotected.Route("/users", func(r chi.Router) {
			r.Get("/", api.Wrap(userHandler.SearchByName))
			r.Get("/me", api.Wrap(userHandler.GetMe))
		})

		rprotected.Route("/transactions", func(r chi.Router) {
			r.Get("/", api.Wrap(transactionHandler.List))
			r.Post("/", api.Wrap(transactionHandler.Create))
			r.Post("/topup", api.Wrap(transactionHandler.TopUp))

			r.Get("/{id}", api.Wrap(transactionHandler.GetByID))
			r.Delete("/{id}", api.Wrap(transactionHandler.Cancel))
			r.Patch("/{id}/category", api.Wrap(transactionHandler.SetCategory))
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
		txService:  transactionService,
		scheduler:  scheduler,
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
	t.Helper()

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

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	return result["jwt_token"].(string)
}

// newUser registers a fresh user and returns its token.
func (app *testApp) newUser(t *testing.T, prefix string) string {
	t.Helper()
	return app.registerUser(t, fmt.Sprintf("%s-%s@example.com", prefix, uuid.New()), testUserPassword)
}

func (app *testApp) loginUser(t *testing.T, email, password string) string {
	t.Helper()

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

	var result map[string]any
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

func userIDFromToken(token string) uuid.UUID {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return uuid.Nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return uuid.Nil
	}
	var claims map[string]any
	json.Unmarshal(payload, &claims)
	sub, _ := claims["sub"].(string)
	id, _ := uuid.Parse(sub)
	return id
}

// do issues an authenticated request and returns the decoded body plus status.
func (app *testApp) do(
	t *testing.T,
	method, path, token string,
	payload any,
) (map[string]any, int, string) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, _ := json.Marshal(payload)
		body = bytes.NewBuffer(raw)
	}

	req, err := http.NewRequest(method, app.addr+path, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(raw, &result)

	return result, resp.StatusCode, string(raw)
}

func (app *testApp) getMe(t *testing.T, token string) map[string]any {
	t.Helper()
	result, status, raw := app.do(t, http.MethodGet, "/users/me", token, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /users/me returned %d: %s", status, raw)
	}
	return result
}

func (app *testApp) getBalance(t *testing.T, token string) int64 {
	t.Helper()
	me := app.getMe(t, token)
	balance, _ := me["balance"].(float64)
	return int64(balance)
}

// topUp calls POST /transactions/topup and returns the decoded body and status.
func (app *testApp) topUp(t *testing.T, token string, amount int64) (map[string]any, int, string) {
	t.Helper()
	return app.do(t, http.MethodPost, "/transactions/topup", token, map[string]any{
		"amount_in_piastres": amount,
	})
}

// fund tops a user up and blocks until the Stripe webhook has credited it.
func (app *testApp) fund(t *testing.T, token string, amount int64) {
	t.Helper()

	before := app.getBalance(t, token)

	result, status, raw := app.topUp(t, token, amount)
	if status != http.StatusOK {
		t.Fatalf("topup failed with status %d: %s", status, raw)
	}

	paymentID, _ := result["provider_payment_id"].(string)
	if paymentID == "" {
		t.Fatalf("topup returned no provider_payment_id: %s", raw)
	}

	if err := app.confirmPayment(paymentID); err != nil {
		t.Fatalf("confirm payment failed: %v", err)
	}

	if err := waitFor(30*time.Second, func() bool {
		return app.getBalance(t, token) >= before+amount
	}); err != nil {
		t.Fatalf("top-up of %d never landed: %v", amount, err)
	}
}

// transfer posts an immediate transfer and returns the status code.
func (app *testApp) transfer(
	t *testing.T,
	token string,
	to uuid.UUID,
	amount int64,
) (map[string]any, int, string) {
	t.Helper()
	return app.do(t, http.MethodPost, "/transactions", token, map[string]any{
		"receiver_id":        to.String(),
		"amount_in_piastres": amount,
	})
}

func waitFor(timeout time.Duration, checkFn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if checkFn() {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for condition")
}

// assertZeroSum is the global invariant: Stripe is a system user, so every
// transaction row nets to zero and the balances of all users must always sum
// to zero. Every test asserts it as a post-condition.
func assertZeroSum(t *testing.T) {
	t.Helper()

	var total int64
	err := pgPool.QueryRow(context.Background(), "SELECT COALESCE(SUM(balance), 0) FROM users").
		Scan(&total)
	if err != nil {
		t.Fatalf("sum balances: %v", err)
	}
	if total != 0 {
		t.Errorf("INVARIANT VIOLATED: SUM(balance) = %d, want 0", total)
	}
}

// statusOf reads a transaction's status straight from the database.
func statusOf(t *testing.T, id uuid.UUID) string {
	t.Helper()

	var status string
	err := pgPool.QueryRow(context.Background(), "SELECT status FROM transactions WHERE id = $1", id).
		Scan(&status)
	if err != nil {
		t.Fatalf("read status of %s: %v", id, err)
	}
	return status
}
