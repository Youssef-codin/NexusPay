package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
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
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/go-chi/jwtauth/v5"
	"github.com/redis/go-redis/v9"
)

func (app *application) mount() http.Handler {
	host, portStr, err := net.SplitHostPort(app.redisOpts.Addr)
	if err != nil {
		panic(err)
	}
	port, _ := strconv.Atoi(portStr)

	rmain := chi.NewRouter()
	rmain.NotFound(func(w http.ResponseWriter, r *http.Request) {
		api.Error(w, "route not found", http.StatusNotFound)
	})

	rmain.Use(middleware.RequestID)
	rmain.Use(middleware.RealIP)
	rmain.Use(middleware.Logger)
	rmain.Use(middleware.Recoverer)
	rmain.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	rmain.Use(middleware.Timeout(60 * time.Second))

	const refreshTokenDuration = 7 * 24 * time.Hour
	authenticator := security.NewAuthenticator(app.config.secret, refreshTokenDuration)

	UserCache := redisDb.NewUsers(app.redis)

	PaymentService := stripe.NewService(app.config.stripe.apiKey)

	TransactionRepo := transactions.NewTransactionRepo(app.db)
	TransactionsService := transactions.NewService(TransactionRepo)

	WalletRepo := wallet.NewWalletRepo(app.db)
	WalletService := wallet.NewService(app.db, WalletRepo, TransactionsService, PaymentService)
	WalletController := wallet.NewController(WalletService)

	AuthRepo := auth.NewAuthRepo(app.db)
	AuthService := auth.NewService(app.db, AuthRepo, UserCache, authenticator, WalletService)
	AuthController := auth.NewController(AuthService)

	UserRepo := users.NewUserRepo(app.db)
	UserService := users.NewService(UserRepo, UserCache)
	UserController := users.NewController(UserService)

	WebhookService := stripe.NewWebhookService(app.db, WalletService, TransactionsService)
	WebhookController := stripe.NewWebhookController(
		app.config.stripe.webhookSecret,
		WebhookService,
	)

	rmain.Group(func(rpublic chi.Router) {
		rpublic.Use(httprate.Limit(
			15,
			time.Minute,
			httprate.WithKeyByIP(),
			httprateredis.WithRedisLimitCounter(&httprateredis.Config{
				Client: app.redis,
			}),
		))

		rpublic.Get("/healthx", func(w http.ResponseWriter, r *http.Request) {
			api.Respond(w, nil, http.StatusNoContent)
		})

		rpublic.Route("/auth", func(rauth chi.Router) {
			rauth.Post("/register", api.Wrap(AuthController.RegisterController))
			rauth.Post("/login", api.Wrap(AuthController.LoginController))
			rauth.Post("/refresh", api.Wrap(AuthController.RefreshController))
		})
	})

	rmain.Group(func(rprotected chi.Router) {
		rprotected.Use(jwtauth.Verifier(authenticator.TokenAuth))
		rprotected.Use(authenticator.AuthHandler())
		rprotected.Use(api.NewUserLimiter(50, app.redis))

		rprotected.Route("/users", func(r chi.Router) {
			r.Get("/test", api.Wrap(AuthController.TestAuth))
			r.Post("/logout", api.Wrap(AuthController.LogoutController))
			r.Get("/", api.Wrap(UserController.SearchByNameController))
		})

		rprotected.Route("/wallet", func(r chi.Router) {
			r.Get("/", api.Wrap(WalletController.GetByUserId))
			r.Patch("/", api.Wrap(WalletController.TopUp))
		})
	})

	rmain.Group(func(rwebhooks chi.Router) {
		rwebhooks.Use(httprate.Limit(
			100,
			time.Minute,
			httprate.WithKeyByIP(),
			httprateredis.WithRedisLimitCounter(&httprateredis.Config{
				Host: host,
				Port: uint16(port),
			}),
		))
		rwebhooks.Route("/webhook", func(r chi.Router) {
			r.Post("/stripe", api.Wrap(WebhookController.Handle))
		})
	})

	log.Printf("Server has started at %v", app.config.addr)
	return rmain
}

func (app *application) run(h http.Handler) error {
	srv := &http.Server{
		Addr:         app.config.addr,
		Handler:      h,
		WriteTimeout: time.Second * 30,
		ReadTimeout:  time.Second * 10,
		IdleTimeout:  time.Minute,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

type application struct {
	config    config
	db        *db.DB
	redis     *redis.Client
	redisOpts *redis.Options
}

type stripeConfig struct {
	apiKey        string
	webhookSecret string
}

type config struct {
	addr   string
	db     dbConfig
	redis  dbConfig
	secret string
	stripe stripeConfig
}

type dbConfig struct {
	dsn string
}
