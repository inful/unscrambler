package explain

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"dagame/internal/explain/viewmodel"
	explainviews "dagame/views/explain"
)

const cookiePrefix = "explain_player"

// Handler holds the store and serves HTTP.
type Handler struct {
	store *Store
}

// NewHandler returns a new handler for the explain game.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts explain routes on r.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.home)
	r.Post("/games", h.createGame)
	r.Route("/game/{id}", func(r chi.Router) {
		r.Get("/", h.gamePage)
		r.Get("/lobby", h.lobbyFragment)
		r.Post("/join", h.joinGame)
		r.Post("/start", h.startGame)
		r.Get("/stream", h.stream)
		r.Get("/round", h.roundFragment)
		r.Get("/canvas", h.canvasFragment)
		r.Get("/players", h.playersFragment)
		r.Get("/scores", h.scoresFragment)
		r.Get("/wordhint", h.wordHintFragment)
		r.Post("/canvas", h.updateCanvas)
		r.Post("/guess", h.submitGuess)
	})
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	renderPage(w, r.Context(), explainviews.HomePage())
}

func (h *Handler) createGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rounds := parseInt(r.FormValue("rounds"), 3)
	durationSec := parseInt(r.FormValue("duration"), 90)
	emojis := parseInt(r.FormValue("emojis"), DefaultEmojisPerRound)
	if rounds < 1 {
		rounds = 1
	}
	if rounds > 10 {
		rounds = 10
	}
	if durationSec < 30 {
		durationSec = 30
	}
	if durationSec > 300 {
		durationSec = 300
	}
	if emojis < 4 {
		emojis = 4
	}
	if emojis > 20 {
		emojis = 20
	}
	g := h.store.CreateGame(rounds, time.Duration(durationSec)*time.Second, "en", emojis)
	http.Redirect(w, r, "/game/"+g.ID, http.StatusSeeOther)
}

func (h *Handler) gamePage(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	playerName, hasPlayer := "", false
	if playerID != "" {
		playerName, hasPlayer = g.PlayerName(playerID)
	}
	snap := g.Snapshot(time.Now().UTC(), playerID)
	isOwner := g.IsOwner(playerID)
	showStart := hasPlayer && isOwner && snap.Status == StatusLobby && len(g.Players) >= MinPlayers

	data := viewmodel.GamePageData{
		GameID:     gameID,
		InviteURL:  buildInviteURL(r, gameID),
		HasPlayer:  hasPlayer,
		PlayerName: playerName,
		PlayerID:   playerID,
		Snap:       snapToVM(snap, showStart, len(g.Players)),
	}
	renderPage(w, r.Context(), explainviews.GamePage(data))
}

func (h *Handler) lobbyFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	_, hasPlayer := g.PlayerName(playerID)
	isOwner := g.IsOwner(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	showStart := hasPlayer && isOwner && snap.Status == StatusLobby && len(g.Players) >= MinPlayers
	vm := snapToVM(snap, showStart, len(g.Players))

	renderFragment(w, r.Context(), explainviews.LobbyFragment(vm, gameID))
}

func (h *Handler) joinGame(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
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
	p := g.AddPlayer(username)
	setPlayerCookie(w, gameID, p.ID)
	h.store.Publish(gameID, "players")
	h.store.Publish(gameID, "lobby")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *Handler) startGame(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	if playerID == "" {
		log.Printf("[explain] start: no player cookie for game %s", gameID)
		http.Error(w, "not a player", http.StatusForbidden)
		return
	}
	if !g.IsOwner(playerID) {
		log.Printf("[explain] start: player %s is not owner of game %s", playerID, gameID)
		http.Error(w, "not the owner", http.StatusForbidden)
		return
	}
	if err := g.Start(time.Now().UTC()); err != nil {
		log.Printf("[explain] start: %v", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.store.EnsureRoundLoop(gameID, g)
	// Publish events; the SSE stream on every client updates the page in place — no navigation required.
	h.store.Publish(gameID, "lobby") // empties #lobby-actions on all clients
	h.store.Publish(gameID, "round")
	h.store.Publish(gameID, "canvas")
	h.store.Publish(gameID, "wordhint")
	h.store.Publish(gameID, "players")
	h.store.Publish(gameID, "scores")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	playerID := getPlayerID(r, gameID)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	hub := h.store.Broadcaster(gameID)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	ctx := r.Context()

	sendAll := func() {
		snap := g.Snapshot(time.Now().UTC(), playerID)
		showStart := playerID != "" && g.IsOwner(playerID) && snap.Status == StatusLobby && len(snap.Players) >= MinPlayers
		vm := snapToVM(snap, showStart, len(snap.Players))
		lobbyHTML := ""
		if snap.Status == StatusLobby {
			lobbyHTML = renderComponent(ctx, explainviews.LobbyFragment(vm, gameID))
		}
		writeSSE(w, "lobby", lobbyHTML)
		writeSSE(w, "round", renderComponent(ctx, explainviews.RoundFragment(vm)))
		writeSSE(w, "canvas", renderComponent(ctx, explainviews.CanvasFragment(vm)))
		writeSSE(w, "wordhint", renderComponent(ctx, explainviews.WordHintFragment(vm, gameID)))
		writeSSE(w, "players", renderComponent(ctx, explainviews.PlayersFragment(vm, playerID)))
		writeSSE(w, "scores", renderComponent(ctx, explainviews.ScoresFragment(vm)))
		flusher.Flush()
	}
	sendAll()

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-sub:
			snap := g.Snapshot(time.Now().UTC(), playerID)
			showStart := playerID != "" && g.IsOwner(playerID) && snap.Status == StatusLobby && len(snap.Players) >= MinPlayers
			vm := snapToVM(snap, showStart, len(snap.Players))
			switch event {
			case "lobby":
				lobbyHTML := ""
				if snap.Status == StatusLobby {
					lobbyHTML = renderComponent(ctx, explainviews.LobbyFragment(vm, gameID))
				}
				writeSSE(w, "lobby", lobbyHTML)
			case "round":
				writeSSE(w, "round", renderComponent(ctx, explainviews.RoundFragment(vm)))
			case "canvas":
				writeSSE(w, "canvas", renderComponent(ctx, explainviews.CanvasFragment(vm)))
			case "wordhint":
				writeSSE(w, "wordhint", renderComponent(ctx, explainviews.WordHintFragment(vm, gameID)))
			case "players":
				writeSSE(w, "players", renderComponent(ctx, explainviews.PlayersFragment(vm, playerID)))
			case "scores":
				writeSSE(w, "scores", renderComponent(ctx, explainviews.ScoresFragment(vm)))
			}
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (h *Handler) roundFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.RoundFragment(snapToVM(snap, false, 0)))
}

func (h *Handler) canvasFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.CanvasFragment(snapToVM(snap, false, 0)))
}

func (h *Handler) wordHintFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.WordHintFragment(snapToVM(snap, false, 0), gameID))
}

func (h *Handler) playersFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.PlayersFragment(snapToVM(snap, false, 0), playerID))
}

func (h *Handler) scoresFragment(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.ScoresFragment(snapToVM(snap, false, 0)))
}

func (h *Handler) updateCanvas(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	if playerID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var items []CanvasItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if g.UpdateCanvas(playerID, items) {
		h.store.Publish(gameID, "canvas")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) submitGuess(w http.ResponseWriter, r *http.Request) {
	gameID := chi.URLParam(r, "id")
	g, ok := h.store.GetGame(gameID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	playerID := getPlayerID(r, gameID)
	if playerID == "" {
		http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	guess := strings.TrimSpace(r.FormValue("guess"))
	correct, err := g.SubmitGuess(playerID, guess, time.Now().UTC())
	if err != nil {
		log.Printf("submit guess: %v", err)
	}
	if correct {
		h.store.Wake(gameID)
		h.store.Publish(gameID, "round")
		h.store.Publish(gameID, "scores")
		h.store.Publish(gameID, "players")
		h.store.Publish(gameID, "wordhint")
	}
	w.WriteHeader(http.StatusNoContent)
}

func getPlayerID(r *http.Request, gameID string) string {
	cookie, err := r.Cookie(cookiePrefix + "_" + gameID)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setPlayerCookie(w http.ResponseWriter, gameID, playerID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookiePrefix + "_" + gameID,
		Value:    playerID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
		Secure:   false, // set true when using HTTPS
	})
}

func buildInviteURL(r *http.Request, gameID string) string {
	if base := strings.TrimSpace(os.Getenv("BASE_URL")); base != "" {
		return strings.TrimRight(base, "/") + "/game/" + gameID
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/game/" + gameID
}

func parseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func writeSSE(w http.ResponseWriter, event, data string) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	for _, line := range strings.Split(data, "\n") {
		_, _ = w.Write([]byte("data: " + line + "\n"))
	}
	_, _ = w.Write([]byte("\n"))
}

// renderPage renders a full-page templ component to the response writer.
func renderPage(w http.ResponseWriter, ctx context.Context, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(ctx, w); err != nil {
		log.Printf("renderPage: %v", err)
	}
}

// renderFragment renders an HTML fragment templ component to the response writer.
func renderFragment(w http.ResponseWriter, ctx context.Context, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(ctx, w); err != nil {
		log.Printf("renderFragment: %v", err)
	}
}

// renderComponent renders a templ component to a string (used for SSE payloads).
func renderComponent(ctx context.Context, c templ.Component) string {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		log.Printf("renderComponent: %v", err)
	}
	return buf.String()
}

// snapToVM converts a domain Snapshot into the view-layer SnapData.
func snapToVM(snap Snapshot, showStart bool, playerCount int) viewmodel.SnapData {
	players := make([]viewmodel.PlayerInfo, len(snap.Players))
	for i, p := range snap.Players {
		players[i] = viewmodel.PlayerInfo{ID: p.ID, Name: p.Name, IsExplainer: p.IsExplainer}
	}
	scores := make([]viewmodel.ScoreEntry, len(snap.Scores))
	for i, s := range snap.Scores {
		scores[i] = viewmodel.ScoreEntry{Name: s.Name, Points: s.Points}
	}
	canvas := make([]viewmodel.CanvasItem, len(snap.Canvas))
	for i, c := range snap.Canvas {
		canvas[i] = viewmodel.CanvasItem{ID: c.ID, Emoji: c.Emoji, X: c.X, Y: c.Y}
	}
	var roundStartedMs, nextRoundAtMs int64
	if !snap.RoundStarted.IsZero() {
		roundStartedMs = snap.RoundStarted.UnixMilli()
	}
	if !snap.NextRoundAt.IsZero() {
		nextRoundAtMs = snap.NextRoundAt.UnixMilli()
	}
	return viewmodel.SnapData{
		Status:           snap.Status,
		CurrentRound:     snap.CurrentRound,
		Rounds:           snap.Rounds,
		RoundDurationSec: int(snap.RoundDuration.Seconds()),
		RoundStartedMs:   roundStartedMs,
		NextRoundAtMs:    nextRoundAtMs,
		ExplainerName:    snap.ExplainerName,
		RoundWinnerName:  snap.RoundWinnerName,
		WinnerName:       snap.WinnerName,
		IsExplainer:      snap.IsExplainer,
		IsGuesser:        snap.IsGuesser,
		Word:             snap.Word,
		RevealedWord:     snap.RevealedWord,
		WordLength:       snap.WordLength,
		Canvas:           canvas,
		RoundEmojis:      snap.RoundEmojis,
		Players:          players,
		Scores:           scores,
		ShowStart:        showStart,
		PlayerCount:      playerCount,
		MinPlayers:       MinPlayers,
	}
}
