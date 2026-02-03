package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"dagame/internal/game"
	"dagame/internal/viewmodel"
	"dagame/views/components"
	"dagame/views/pages"
)

type GameHandler struct {
	store *game.Store
}

func NewGameHandler(store *game.Store) *GameHandler {
	return &GameHandler{store: store}
}

func (h *GameHandler) RegisterRoutes(r chi.Router) {
	r.Route("/game/{id}", func(r chi.Router) {
		r.Get("/", h.gamePage)
		r.Post("/join", h.joinGame)
		r.Post("/start", h.startGame)
		r.Post("/restart", h.restartGame)
		r.Get("/round", h.roundFragment)
		r.Get("/players", h.playersFragment)
		r.Get("/scores", h.scoresFragment)
		r.Get("/stream", h.stream)
		r.Post("/progress", h.progressUpdate)
		r.Post("/guess", h.submitGuess)
	})
}

func (h *GameHandler) gamePage(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	playerName, hasPlayer := h.findPlayerName(r, instance)
	playerID := playerIDFromCookie(r, gameID)
	isOwner := instance.IsOwner(playerID)
	inviteURL := buildInviteURL(r, gameID)
	snapshot := instance.Snapshot(time.Now().UTC())
	showStart := hasPlayer && isOwner && snapshot.Status == game.StatusLobby
	duration := int(snapshot.RoundDuration.Seconds())

	data := viewmodel.GamePage{
		Title:          "Dagame",
		GameID:         gameID,
		InviteURL:      inviteURL,
		Players:        toPlayerProgress(snapshot.Progress, ""),
		HasPlayer:      hasPlayer,
		PlayerName:     playerName,
		IsOwner:        isOwner,
		Rounds:         snapshot.Rounds,
		RoundDuration:  duration,
		Status:         snapshot.Status,
		ShowStart:      showStart,
		Scores:         toScoreEntries(snapshot.Scores),
		WinnerName:     snapshot.WinnerName,
		CurrentRound:   snapshot.CurrentRound,
		TotalRounds:    snapshot.Rounds,
		RoundStartedMs: snapshot.RoundStarted.UnixMilli(),
		Scrambled:      snapshot.RoundData.Scrambled,
		TargetWord:     snapshot.RoundData.Word,
		WordLength:     snapshot.WordLength,
	}
	render(w, r, pages.GamePage(data))
}

func (h *GameHandler) joinGame(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}
	if len(username) > 20 {
		username = username[:20]
	}

	player := instance.AddPlayer(username)

	setPlayerCookie(w, gameID, player.ID)
	h.store.Publish(gameID, "players")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *GameHandler) startGame(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := playerIDFromCookie(r, gameID)
	if !instance.IsOwner(playerID) {
		http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
		return
	}
	_ = instance.Start(time.Now().UTC())
	h.store.EnsureRoundLoop(gameID, instance)
	h.store.Publish(gameID, "round")
	h.store.Publish(gameID, "scores")
	h.store.Publish(gameID, "players")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *GameHandler) restartGame(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := playerIDFromCookie(r, gameID)
	if !instance.IsOwner(playerID) {
		http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
		return
	}
	instance.Restart(time.Now().UTC())
	h.store.EnsureRoundLoop(gameID, instance)
	h.store.Publish(gameID, "round")
	h.store.Publish(gameID, "scores")
	h.store.Publish(gameID, "players")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *GameHandler) roundFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	now := time.Now().UTC()
	snapshot := instance.Snapshot(now)
	data := buildRoundFragment(gameID, snapshot)

	render(w, r, components.RoundFragment(data))
}

func (h *GameHandler) scoresFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	snapshot := instance.Snapshot(time.Now().UTC())
	data := viewmodel.ScoresFragment{
		GameID:     gameID,
		Scores:     toScoreEntries(snapshot.Scores),
		WinnerName: snapshot.WinnerName,
		Status:     snapshot.Status,
		IsOwner:    instance.IsOwner(playerIDFromCookie(r, gameID)),
	}
	render(w, r, components.ScoresFragment(data))
}

func (h *GameHandler) playersFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	playerName, _ := h.findPlayerName(r, instance)
	snapshot := instance.Snapshot(time.Now().UTC())
	data := viewmodel.PlayersFragment{
		Players:    toPlayerProgress(snapshot.Progress, playerName),
		WordLength: snapshot.WordLength,
		PlayerName: playerName,
	}
	render(w, r, components.PlayersFragment(data))
}

func (h *GameHandler) submitGuess(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := playerIDFromCookie(r, gameID)
	if playerID == "" {
		http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	guess := r.FormValue("guess")
	debugSnapshot := instance.Snapshot(time.Now().UTC())
	log.Printf("submit guess debug game=%s roundWord=%q scrambled=%q", gameID, debugSnapshot.RoundData.Word, debugSnapshot.RoundData.Scrambled)
	ok, err := instance.SubmitGuess(playerID, guess, time.Now().UTC())
	if err != nil {
		log.Printf("submit guess error game=%s player=%s err=%v", gameID, playerID, err)
	}
	log.Printf("submit guess game=%s player=%s guess=%q ok=%t", gameID, playerID, guess, ok)
	if ok {
		h.store.WakeRoundLoop(gameID)
		h.store.Publish(gameID, "round")
		h.store.Publish(gameID, "scores")
		h.store.Publish(gameID, "players")
	}
	if r.Header.Get("Hx-Request") == "true" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *GameHandler) progressUpdate(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := playerIDFromCookie(r, gameID)
	if playerID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	guess := r.FormValue("guess")
	snapshot := instance.Snapshot(time.Now().UTC())
	correctIndexes := correctIndexesForGuess(snapshot.RoundData.Word, guess)
	instance.UpdateProgress(playerID, len(correctIndexes), time.Now().UTC())
	h.store.Publish(gameID, "players")
	writeJSON(w, map[string]any{
		"correctIndexes": correctIndexes,
	})
}

func (h *GameHandler) stream(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	instance, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	playerID := playerIDFromCookie(r, gameID)
	playerName, _ := h.findPlayerName(r, instance)

	hub := h.store.Broadcaster(gameID)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	sendSnapshot := func(includeRound bool, includePlayers bool, includeScores bool) {
		snapshot := instance.Snapshot(time.Now().UTC())
		if includeRound {
			roundHTML := renderToString(r, components.RoundFragment(buildRoundFragment(gameID, snapshot)))
			writeSSE(w, "round", roundHTML)
		}
		if includePlayers {
			playersHTML := renderToString(r, components.PlayersFragment(viewmodel.PlayersFragment{
				Players:    toPlayerProgress(snapshot.Progress, playerName),
				WordLength: snapshot.WordLength,
				PlayerName: playerName,
			}))
			writeSSE(w, "players", playersHTML)
		}
		if includeScores {
			scoresHTML := renderToString(r, components.ScoresFragment(viewmodel.ScoresFragment{
				GameID:     gameID,
				Scores:     toScoreEntries(snapshot.Scores),
				WinnerName: snapshot.WinnerName,
				Status:     snapshot.Status,
				IsOwner:    instance.IsOwner(playerID),
			}))
			writeSSE(w, "scores", scoresHTML)
		}
		flusher.Flush()
	}

	sendSnapshot(true, true, true)

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-sub:
			switch event {
			case "players":
				sendSnapshot(false, true, false)
			case "scores":
				sendSnapshot(false, false, true)
			case "round":
				sendSnapshot(true, false, false)
			}
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (h *GameHandler) findPlayerName(r *http.Request, instance *game.Game) (string, bool) {
	playerID := playerIDFromCookie(r, instance.ID)
	if playerID == "" {
		return "", false
	}
	return instance.PlayerName(playerID)
}

func playerIDFromCookie(r *http.Request, gameID string) string {
	cookie, err := r.Cookie(playerCookieName(gameID))
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setPlayerCookie(w http.ResponseWriter, gameID string, playerID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     playerCookieName(gameID),
		Value:    playerID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
}

func playerCookieName(gameID string) string {
	return "dagame_player_" + gameID
}

func buildInviteURL(r *http.Request, gameID string) string {
	if baseURL := strings.TrimSpace(os.Getenv("BASE_URL")); baseURL != "" {
		return strings.TrimRight(baseURL, "/") + "/game/" + gameID
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	return scheme + "://" + host + "/game/" + gameID
}

func toScoreEntries(scores []game.ScoreEntry) []viewmodel.ScoreEntry {
	out := make([]viewmodel.ScoreEntry, 0, len(scores))
	for _, entry := range scores {
		out = append(out, viewmodel.ScoreEntry{
			Name:   entry.Name,
			Points: entry.Points,
		})
	}
	return out
}

func toPlayerProgress(entries []game.PlayerProgress, excludeName string) []viewmodel.PlayerProgress {
	out := make([]viewmodel.PlayerProgress, 0, len(entries))
	for _, entry := range entries {
		if excludeName != "" && entry.Name == excludeName {
			continue
		}
		out = append(out, viewmodel.PlayerProgress{
			Name:    entry.Name,
			Correct: entry.Correct,
		})
	}
	return out
}

func buildRoundFragment(gameID string, snapshot game.Snapshot) viewmodel.RoundFragment {
	expired := snapshot.Status == game.StatusInProgress && !snapshot.RoundEndedAt.IsZero()
	return viewmodel.RoundFragment{
		GameID:         gameID,
		Status:         snapshot.Status,
		CurrentRound:   snapshot.CurrentRound,
		TotalRounds:    snapshot.Rounds,
		RoundStartedMs: snapshot.RoundStarted.UnixMilli(),
		DurationSec:    int(snapshot.RoundDuration.Seconds()),
		Scrambled:      snapshot.RoundData.Scrambled,
		TargetWord:     snapshot.RoundData.Word,
		Expired:        expired,
		RoundWinner:    snapshot.RoundWinner,
		RoundEndedMs:   snapshot.RoundEndedAt.UnixMilli(),
		NextRoundMs:    snapshot.NextRoundAt.UnixMilli(),
		RoundLocked:    snapshot.RoundWinner != "" || expired,
		RoundKey:       buildRoundKey(snapshot),
	}
}

func buildRoundKey(snapshot game.Snapshot) string {
	return strings.Join([]string{
		snapshot.Status,
		strconv.Itoa(snapshot.CurrentRound),
		strconv.FormatInt(snapshot.RoundStarted.UnixMilli(), 10),
		strconv.FormatInt(snapshot.RoundEndedAt.UnixMilli(), 10),
		snapshot.RoundWinner,
	}, "|")
}

func correctIndexesForGuess(word string, guess string) []int {
	if word == "" || guess == "" {
		return []int{}
	}
	wordRunes := []rune(word)
	guessRunes := []rune(guess)
	limit := len(wordRunes)
	if len(guessRunes) < limit {
		limit = len(guessRunes)
	}
	indexes := make([]int, 0, limit)
	for i := 0; i < limit; i++ {
		if guessRunes[i] == wordRunes[i] {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func renderToString(r *http.Request, component templ.Component) string {
	var buf bytes.Buffer
	_ = component.Render(r.Context(), &buf)
	return buf.String()
}

func writeSSE(w http.ResponseWriter, event string, data string) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	for _, line := range strings.Split(data, "\n") {
		_, _ = w.Write([]byte("data: " + line + "\n"))
	}
	_, _ = w.Write([]byte("\n"))
}
