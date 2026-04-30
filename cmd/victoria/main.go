package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"victoria/internal/app"
	"victoria/internal/httpapi"
	"victoria/internal/store/memory"
	"victoria/internal/store/postgres"
)

func main() {
	addr := os.Getenv("VICTORIA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	var store app.Store
	if dsn := os.Getenv("VICTORIA_DATABASE_URL"); dsn != "" {
		pgStore, err := postgres.Connect(context.Background(), dsn)
		if err != nil {
			log.Fatal(err)
		}
		defer pgStore.Close()
		store = pgStore
		log.Print("using postgres store")
	} else {
		store = memory.New()
		log.Print("using in-memory store")
	}
	application := app.New(store)
	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(application).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("victoria listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
