// Command ackermann-server runs the standalone Ackermann steering geometry
// HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ackermann/internal/api"
	"ackermann/internal/service"
	"ackermann/internal/store"
)

type config struct {
	addr        string
	storage     string // "memory" | "postgres"
	dsn         string
	toleranceMM float64
}

func main() {
	cfg := config{
		addr:        envOr("ADDR", ":8080"),
		storage:     envOr("STORAGE", "memory"),
		dsn:         envOr("DATABASE_DSN", "postgres://ackermann:ackermann@localhost:5432/ackermann?sslmode=disable"),
		toleranceMM: envFloat("ICR_TOLERANCE_MM", 1e-6),
	}

	flag.StringVar(&cfg.addr, "addr", cfg.addr, "listen address")
	flag.StringVar(&cfg.storage, "storage", cfg.storage, "storage backend: memory|postgres")
	flag.StringVar(&cfg.dsn, "dsn", cfg.dsn, "PostgreSQL connection string")
	flag.Float64Var(&cfg.toleranceMM, "icr-tolerance-mm", cfg.toleranceMM,
		"accepted instant-center concurrency residual in millimeters")
	flag.Parse()

	logger := log.New(os.Stdout, "ackermann: ", log.LstdFlags|log.Lmsgprefix)

	if cfg.toleranceMM <= 0 {
		logger.Fatalf("icr-tolerance-mm must be positive, got %g", cfg.toleranceMM)
	}

	st, err := buildStore(cfg, logger)
	if err != nil {
		logger.Fatalf("storage init: %v", err)
	}
	defer func() { _ = st.Close() }()

	svc := service.New(st, service.Config{
		AngleUnit:       "deg",
		OutputAngleUnit: "deg",
		ToleranceMM:     cfg.toleranceMM,
		ToleranceDeg:    1e-9,
	})

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api.NewServer(svc, st, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Printf("listening on %s (storage=%s)", cfg.addr, cfg.storage)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	logger.Printf("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown: %v", err)
	}
}

func buildStore(cfg config, logger *log.Logger) (store.Store, error) {
	switch cfg.storage {
	case "memory":
		logger.Printf("using in-memory storage (history is cleared on restart)")
		return store.NewMemory(), nil
	case "postgres":
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return withRetry(ctx, logger, func(ctx context.Context) (store.Store, error) {
			return store.NewPostgres(ctx, cfg.dsn)
		})
	default:
		return nil, errors.New("unknown storage backend \"" + cfg.storage + "\" (want memory or postgres)")
	}
}

// withRetry tolerates the database container starting after the app, as is
// common under docker compose.
func withRetry(parent context.Context, logger *log.Logger,
	open func(context.Context) (store.Store, error)) (store.Store, error) {

	delay := time.Second
	for attempt := 1; ; attempt++ {
		st, err := open(parent)
		if err == nil {
			return st, nil
		}
		if parent.Err() != nil || attempt >= 30 {
			return nil, err
		}
		logger.Printf("database not ready (attempt %d): %v; retrying in %s", attempt, err, delay)
		select {
		case <-parent.Done():
			return nil, parent.Err()
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
