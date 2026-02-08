package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"dagame/internal/game"
	"dagame/internal/viewmodel"
	"dagame/views/pages"
)

type HomeHandler struct {
	store *game.Store
}

// NewHomeHandler builds the handler for the landing page and game creation.
func NewHomeHandler(store *game.Store) *HomeHandler {
	return &HomeHandler{store: store}
}

// RegisterRoutes wires the home endpoints.
func (h *HomeHandler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.home)
	r.Post("/games", h.createGame)
}

var langLabels = map[string]string{
	"en": "English",
	"no": "Norwegian",
}

func (h *HomeHandler) home(w http.ResponseWriter, r *http.Request) {
	langs := game.SupportedLanguages()
	opts := make([]viewmodel.LanguageOption, 0, len(langs))
	for _, code := range langs {
		label := code
		if l, ok := langLabels[code]; ok {
			label = l
		}
		opts = append(opts, viewmodel.LanguageOption{Code: code, Label: label})
	}
	render(w, r, pages.HomePage(opts))
}

func (h *HomeHandler) createGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rounds := parseInt(r.FormValue("rounds"), 5)
	durationSec := parseInt(r.FormValue("duration"), 60)
	lang := strings.TrimSpace(r.FormValue("lang"))
	if lang == "" {
		lang = "en"
	}
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

	gameInstance := h.store.CreateGame(rounds, time.Duration(durationSec)*time.Second, lang)
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
