package explain

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"dagame/internal/explain/viewmodel"
	"dagame/pkg/gamecommon"
	"dagame/pkg/httputil"
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
	gameIDFromRoute := func(r *http.Request) string { return chi.URLParam(r, "id") }
	cookieName := func(gameID string) string { return cookiePrefix + "_" + gameID }
	withGame := httputil.WithGame(h.store, cookieName, gameIDFromRoute)
	r.Route("/game/{id}", func(r chi.Router) {
		r.Get("/", withGame(h.gamePage))
		r.Get("/lobby", withGame(h.lobbyFragment))
		r.Post("/join", withGame(h.joinGame))
		r.Post("/start", withGame(h.startGame))
		r.Get("/stream", withGame(h.stream))
		r.Get("/round", withGame(h.roundFragment))
		r.Get("/canvas", withGame(h.canvasFragment))
		r.Get("/players", withGame(h.playersFragment))
		r.Get("/scores", withGame(h.scoresFragment))
		r.Get("/wordhint", withGame(h.wordHintFragment))
		r.Post("/canvas", withGame(h.updateCanvas))
		r.Post("/guess", withGame(h.submitGuess))
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
	rounds := httputil.ParseInt(r.FormValue("rounds"), 3)
	durationSec := httputil.ParseInt(r.FormValue("duration"), 90)
	emojis := httputil.ParseInt(r.FormValue("emojis"), DefaultEmojisPerRound)
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

func (h *Handler) gamePage(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	playerName, hasPlayer := "", false
	if playerID != "" {
		playerName, hasPlayer = g.PlayerName(playerID)
	}
	snap := g.Snapshot(time.Now().UTC(), playerID)
	isOwner := g.IsOwner(playerID)
	showStart := hasPlayer && isOwner && snap.Status == gamecommon.StatusLobby && len(g.Players) >= MinPlayers

	data := viewmodel.GamePageData{
		GameID:     gameID,
		InviteURL:  httputil.BuildInviteURL(r, "/game/", gameID),
		HasPlayer:  hasPlayer,
		PlayerName: playerName,
		PlayerID:   playerID,
		Snap:       snapToVM(snap, showStart, len(g.Players), playerName),
	}
	renderPage(w, r.Context(), explainviews.GamePage(data))
}

func (h *Handler) lobbyFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	playerName, hasPlayer := g.PlayerName(playerID)
	isOwner := g.IsOwner(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	showStart := hasPlayer && isOwner && snap.Status == gamecommon.StatusLobby && len(g.Players) >= MinPlayers
	vm := snapToVM(snap, showStart, len(g.Players), playerName)

	renderFragment(w, r.Context(), explainviews.LobbyFragment(vm, gameID))
}

func (h *Handler) joinGame(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
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
	httputil.SetPlayerCookie(w, cookiePrefix+"_"+gameID, p.ID)
	h.store.Publish(gameID, "players")
	h.store.Publish(gameID, "lobby")
	http.Redirect(w, r, "/game/"+gameID, http.StatusSeeOther)
}

func (h *Handler) startGame(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
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

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	playerName, _ := g.PlayerName(playerID)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	hub := h.store.Broadcaster(gameID)
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	ctx := r.Context()

	sendAll := func() {
		snap := g.Snapshot(time.Now().UTC(), playerID)
		showStart := playerID != "" && g.IsOwner(playerID) && snap.Status == gamecommon.StatusLobby && len(snap.Players) >= MinPlayers
		vm := snapToVM(snap, showStart, len(snap.Players), playerName)
		lobbyHTML := ""
		if snap.Status == gamecommon.StatusLobby {
			lobbyHTML = renderComponent(ctx, explainviews.LobbyFragment(vm, gameID))
		}
		httputil.WriteSSE(w, "lobby", lobbyHTML)
		httputil.WriteSSE(w, "round", renderComponent(ctx, explainviews.RoundFragment(vm)))
		httputil.WriteSSE(w, "canvas", renderComponent(ctx, explainviews.CanvasFragment(vm)))
		httputil.WriteSSE(w, "wordhint", renderComponent(ctx, explainviews.WordHintFragment(vm, gameID)))
		httputil.WriteSSE(w, "players", renderComponent(ctx, explainviews.PlayersFragment(vm, playerID)))
		httputil.WriteSSE(w, "scores", renderComponent(ctx, explainviews.ScoresFragment(vm)))
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
			showStart := playerID != "" && g.IsOwner(playerID) && snap.Status == gamecommon.StatusLobby && len(snap.Players) >= MinPlayers
			vm := snapToVM(snap, showStart, len(snap.Players), playerName)
			switch event {
			case "lobby":
				lobbyHTML := ""
				if snap.Status == gamecommon.StatusLobby {
					lobbyHTML = renderComponent(ctx, explainviews.LobbyFragment(vm, gameID))
				}
				httputil.WriteSSE(w, "lobby", lobbyHTML)
			case "round":
				httputil.WriteSSE(w, "round", renderComponent(ctx, explainviews.RoundFragment(vm)))
			case "canvas":
				httputil.WriteSSE(w, "canvas", renderComponent(ctx, explainviews.CanvasFragment(vm)))
			case "wordhint":
				httputil.WriteSSE(w, "wordhint", renderComponent(ctx, explainviews.WordHintFragment(vm, gameID)))
			case "players":
				httputil.WriteSSE(w, "players", renderComponent(ctx, explainviews.PlayersFragment(vm, playerID)))
			case "scores":
				httputil.WriteSSE(w, "scores", renderComponent(ctx, explainviews.ScoresFragment(vm)))
			}
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}

func (h *Handler) roundFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.RoundFragment(snapToVM(snap, false, 0, pname)))
}

func (h *Handler) canvasFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.CanvasFragment(snapToVM(snap, false, 0, pname)))
}

func (h *Handler) wordHintFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.WordHintFragment(snapToVM(snap, false, 0, pname), gameID))
}

func (h *Handler) playersFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.PlayersFragment(snapToVM(snap, false, 0, pname), playerID))
}

func (h *Handler) scoresFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), explainviews.ScoresFragment(snapToVM(snap, false, 0, pname)))
}

func (h *Handler) updateCanvas(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
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

func (h *Handler) submitGuess(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
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
func snapToVM(snap Snapshot, showStart bool, playerCount int, currentPlayerName string) viewmodel.SnapData {
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
		ShowStart:         showStart,
		PlayerCount:       playerCount,
		MinPlayers:        MinPlayers,
		CurrentPlayerName: currentPlayerName,
	}
}
