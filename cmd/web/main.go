package main

import (
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"dagame/internal/game"
	"dagame/internal/handlers"
)

func main() {
	store := game.NewStore()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	r.Mount("/static", http.StripPrefix("/static", http.FileServer(http.Dir("./static"))))

	homeHandler := handlers.NewHomeHandler(store)
	gameHandler := handlers.NewGameHandler(store)

	homeHandler.RegisterRoutes(r)
	gameHandler.RegisterRoutes(r)

	log.Println("listening on http://localhost:9080")
	if err := http.ListenAndServe(":9080", r); err != nil {
		log.Fatal(err)
	}
}
