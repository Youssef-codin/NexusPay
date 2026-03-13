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
	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/db/redisDb"
	"github.com/Youssef-codin/NexusPay/internal/security"
	"github.com/Youssef-codin/NexusPay/internal/users"
	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	httprateredis "github.com/go-chi/httprate-redis"
	"github.com/go-chi/jwtauth/v5"
	"github.com/jackc/pgx/v5"
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

	// A good base middleware stack
	rmain.Use(middleware.RequestID)
	rmain.Use(middleware.RealIP)
	rmain.Use(middleware.Logger)
	rmain.Use(middleware.Recoverer)
	rmain.Use(cors.Handler(cors.Options{
		// AllowedOrigins:   []string{"https://foo.com"}, // Use this to allow specific origin hosts
		AllowedOrigins: []string{"https://*", "http://*"},
		// AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	}))

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	rmain.Use(middleware.Timeout(60 * time.Second))

	const refreshTokenDuration = 7 * 24 * time.Hour
	authenticator := security.NewAuthenticator(app.config.secret, refreshTokenDuration)

	SQLCRepo := repo.New(app.db)
	UserCache := redisDb.NewUsers(app.redis)
	AuthService := auth.NewService(SQLCRepo, app.db, UserCache, authenticator)
	AuthController := auth.NewController(AuthService)

	rmain.Group(func(rprotected chi.Router) {
		rprotected.Use(jwtauth.Verifier(authenticator.TokenAuth))
		rprotected.Use(authenticator.AuthHandler())
		rprotected.Use(api.NewUserLimiter(15, host, uint16(port)))

		rprotected.Get("/auth/test", api.Wrap(AuthController.TestAuth))
		rprotected.Post("/auth/logout", api.Wrap(AuthController.LogoutController))

		UserService := users.NewService(SQLCRepo, app.db, UserCache)
		UserController := users.NewController(UserService)

		rprotected.Get("/users", api.Wrap(UserController.SearchByNameController))
	})

	rmain.Group(func(rpublic chi.Router) {
		rpublic.Use(httprate.Limit(
			15,
			time.Minute,
			httprate.WithKeyByIP(),
			httprateredis.WithRedisLimitCounter(&httprateredis.Config{
				Host: host,
				Port: uint16(port),
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

	//graceful shutdown
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
	db        *pgx.Conn
	redis     *redis.Client
	redisOpts *redis.Options
}

type config struct {
	addr   string
	db     dbConfig
	redis  dbConfig
	secret string
}

type dbConfig struct {
	dsn string
}
