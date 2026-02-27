package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"dagame/internal/explain"
)

func main() {
	store := explain.NewStore()
	handler := explain.NewHandler(store)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	handler.RegisterRoutes(r)

	addr := ":" + strings.TrimSpace(os.Getenv("PORT"))
	if addr == ":" {
		addr = ":8081"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("Explain game listening on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
