// Command ackermann runs the standalone Ackermann steering geometry HTTP
// service.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"ackermann-service/ackermann"
	"ackermann-service/api"
	"ackermann-service/storage"
)

func main() {
	addr := env("ADDR", ":8080")
	tol := ackermann.DefaultTol

	store := buildStore()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = store.Close(ctx)
	}()

	srv := api.NewServer(store, tol)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("ackermann steering service listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func buildStore() storage.Store {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DSN")
	}
	if dsn == "" {
		log.Printf("no DATABASE_URL set; using in-memory storage (history is not durable)")
		return storage.NewMemoryStore()
	}

	// Wait briefly for a freshly started database container.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		st, err := storage.NewPostgres(ctx, dsn)
		if err == nil {
			log.Printf("connected to PostgreSQL storage")
			return st
		}
		lastErr = err
		log.Printf("waiting for database (%d/15): %v", attempt+1, err)
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("could not connect to database: %v", lastErr)
	return nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
