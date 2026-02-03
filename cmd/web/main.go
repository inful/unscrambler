package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"dagame/internal/game"
	"dagame/internal/handlers"
)

func main() {
	_ = mime.AddExtensionType(".js", "application/javascript")
	_ = mime.AddExtensionType(".css", "text/css")

	store := game.NewStore()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		log.Fatal(err)
	}

	r.Mount("/static", http.StripPrefix("/static", http.FileServer(http.FS(staticFS))))

	homeHandler := handlers.NewHomeHandler(store)
	gameHandler := handlers.NewGameHandler(store)

	homeHandler.RegisterRoutes(r)
	gameHandler.RegisterRoutes(r)

	addr := ":" + strings.TrimSpace(os.Getenv("PORT"))
	if addr == ":" {
		addr = ":8080"
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

//go:embed static/*
var embeddedStatic embed.FS
