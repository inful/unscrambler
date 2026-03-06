package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"dagame/internal/game"
	"dagame/internal/viewmodel"
	"dagame/pkg/gamecommon"
	"dagame/pkg/httputil"
	"dagame/views/components"
	"dagame/views/pages"
)

type GameHandler struct {
	store *game.Store
}

// NewGameHandler builds the handler for game session routes.
func NewGameHandler(store *game.Store) *GameHandler {
	return &GameHandler{store: store}
}

// RegisterRoutes wires game session endpoints.
func (h *GameHandler) RegisterRoutes(r chi.Router) {
	gameIDFromRoute := func(r *http.Request) string { return chi.URLParam(r, "id") }
	withGame := httputil.WithGame(h.store, playerCookieName, gameIDFromRoute)
	r.Route("/game/{id}", func(r chi.Router) {
		r.Get("/", withGame(h.gamePage))
		r.Post("/join", withGame(h.joinGame))
		r.Post("/start", withGame(h.startGame))
		r.Post("/restart", withGame(h.restartGame))
		r.Get("/round", withGame(h.roundFragment))
		r.Get("/players", withGame(h.playersFragment))
		r.Get("/scores", withGame(h.scoresFragment))
		r.Get("/stream", withGame(h.stream))
		r.Post("/progress", withGame(h.progressUpdate))
		r.Post("/guess", withGame(h.submitGuess))
	})
}

func (h *GameHandler) gamePage(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
	playerName, hasPlayer := h.findPlayerName(instance, playerID)
	isOwner := instance.IsOwner(playerID)
	inviteURL := httputil.BuildInviteURL(r, "/game/", gameID)
	snapshot := instance.Snapshot(time.Now().UTC())
	showStart := hasPlayer && isOwner && snapshot.Status == gamecommon.StatusLobby
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

func (h *GameHandler) joinGame(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
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

	httputil.SetPlayerCookie(w, playerCookieName(gameID), player.ID)
	h.store.Publish(gameID, "players")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *GameHandler) startGame(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
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

func (h *GameHandler) restartGame(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
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

func (h *GameHandler) roundFragment(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
	now := time.Now().UTC()
	snapshot := instance.Snapshot(now)
	data := buildRoundFragment(gameID, snapshot)

	render(w, r, components.RoundFragment(data))
}

func (h *GameHandler) scoresFragment(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
	playerName, _ := h.findPlayerName(instance, playerID)
	snapshot := instance.Snapshot(time.Now().UTC())
	data := viewmodel.ScoresFragment{
		GameID:     gameID,
		Scores:     toScoreEntries(snapshot.Scores),
		WinnerName: snapshot.WinnerName,
		Status:     snapshot.Status,
		IsOwner:    instance.IsOwner(playerID),
		PlayerName: playerName,
	}
	render(w, r, components.ScoresFragment(data))
}

func (h *GameHandler) playersFragment(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	playerName, _ := h.findPlayerName(instance, playerID)
	snapshot := instance.Snapshot(time.Now().UTC())
	data := viewmodel.PlayersFragment{
		Players:    toPlayerProgress(snapshot.Progress, playerName),
		WordLength: snapshot.WordLength,
		PlayerName: playerName,
	}
	render(w, r, components.PlayersFragment(data))
}

func (h *GameHandler) submitGuess(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
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

func (h *GameHandler) progressUpdate(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
	if playerID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	// Server computes correct indices so the answer isn't exposed in HTML.
	guess := r.FormValue("guess")
	snapshot := instance.Snapshot(time.Now().UTC())
	correctIndexes := correctIndexesForGuess(snapshot.RoundData.Word, guess)
	instance.UpdateProgress(playerID, len(correctIndexes), time.Now().UTC())
	h.store.Publish(gameID, "players")
	writeJSON(w, map[string]any{
		"correctIndexes": correctIndexes,
	})
}

func (h *GameHandler) stream(w http.ResponseWriter, r *http.Request, instance *game.Game, playerID string) {
	gameID := instance.ID
	playerName, _ := h.findPlayerName(instance, playerID)
	hub := h.store.Broadcaster(gameID)

	onEvent := func(w http.ResponseWriter, ctx context.Context, event string) {
		sendSnapshot := func(includeRound, includePlayers, includeScores bool) {
			snapshot := instance.Snapshot(time.Now().UTC())
			if includeRound {
				roundHTML := renderToString(r, components.RoundFragment(buildRoundFragment(gameID, snapshot)))
				httputil.WriteSSE(w, "round", roundHTML)
			}
			if includePlayers {
				playersHTML := renderToString(r, components.PlayersFragment(viewmodel.PlayersFragment{
					Players:    toPlayerProgress(snapshot.Progress, playerName),
					WordLength: snapshot.WordLength,
					PlayerName: playerName,
				}))
				httputil.WriteSSE(w, "players", playersHTML)
			}
			if includeScores {
				scoresHTML := renderToString(r, components.ScoresFragment(viewmodel.ScoresFragment{
					GameID:     gameID,
					Scores:     toScoreEntries(snapshot.Scores),
					WinnerName: snapshot.WinnerName,
					Status:     snapshot.Status,
					IsOwner:    instance.IsOwner(playerID),
					PlayerName: playerName,
				}))
				httputil.WriteSSE(w, "scores", scoresHTML)
			}
		}
		switch event {
		case "initial":
			sendSnapshot(true, true, true)
		case "round":
			sendSnapshot(true, false, false)
		case "players":
			sendSnapshot(false, true, false)
		case "scores":
			sendSnapshot(false, false, true)
		}
	}

	if err := httputil.SSEStream(w, r, hub, onEvent); err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
	}
}

func (h *GameHandler) findPlayerName(instance *game.Game, playerID string) (string, bool) {
	if playerID == "" {
		return "", false
	}
	return instance.PlayerName(playerID)
}

func playerCookieName(gameID string) string {
	return "dagame_player_" + gameID
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
	expired := snapshot.Status == gamecommon.StatusInProgress && !snapshot.RoundEndedAt.IsZero()
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
