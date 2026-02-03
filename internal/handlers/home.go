package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"dagame/internal/game"
	"dagame/views/pages"
)

type HomeHandler struct {
	store *game.Store
}

func NewHomeHandler(store *game.Store) *HomeHandler {
	return &HomeHandler{store: store}
}

func (h *HomeHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.home)
	r.Post("/games", h.createGame)
}

func (h *HomeHandler) home(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.HomePage())
}

func (h *HomeHandler) createGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rounds := parseInt(r.FormValue("rounds"), 5)
	durationSec := parseInt(r.FormValue("duration"), 60)
	if rounds < 1 {
		rounds = 1
	}
	if rounds > 10 {
		rounds = 10
	}
	if durationSec < 10 {
		durationSec = 10
	}
	if durationSec > 300 {
		durationSec = 300
	}

	gameInstance := h.store.CreateGame(rounds, time.Duration(durationSec)*time.Second)
	http.Redirect(w, r, "/game/"+gameInstance.ID, http.StatusSeeOther)
}

func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
