package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"golangtest/internal/user"
)

func main() {
	addr := envOr("ADDR", ":8080")
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Println("warning: ADMIN_TOKEN is not set; notes endpoints are disabled")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           user.NewHandler(user.NewStore(), adminToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on %s", addr)
	log.Fatal(srv.ListenAndServe())
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
