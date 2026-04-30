package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"victoria/internal/app"
	"victoria/internal/httpapi"
	"victoria/internal/store/memory"
)

func main() {
	addr := os.Getenv("VICTORIA_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	store := memory.New()
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
