package draw

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

	"dagame/internal/draw/viewmodel"
	"dagame/pkg/gamecommon"
	"dagame/pkg/httputil"
	drawviews "dagame/views/draw"
)

const cookiePrefix = "draw_player"

// Handler holds the store and serves HTTP.
type Handler struct {
	store *Store
}

// NewHandler returns a new handler for the draw game.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes mounts draw routes on r.
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
	renderPage(w, r.Context(), drawviews.HomePage())
}

func (h *Handler) createGame(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rounds := httputil.ParseInt(r.FormValue("rounds"), 3)
	durationSec := httputil.ParseInt(r.FormValue("duration"), 90)
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
	g := h.store.CreateGame(rounds, time.Duration(durationSec)*time.Second, "en")
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

	vm := snapToVM(snap, showStart, len(g.Players), playerName)
	vm.StrokesJSON = mustMarshalStrokes(vm.Strokes)
	data := viewmodel.GamePageData{
		GameID:     gameID,
		InviteURL:  httputil.BuildInviteURL(r, "/game/", gameID),
		HasPlayer:  hasPlayer,
		PlayerName: playerName,
		PlayerID:   playerID,
		Snap:       vm,
	}
	renderPage(w, r.Context(), drawviews.GamePage(data))
}

func (h *Handler) lobbyFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	playerName, hasPlayer := g.PlayerName(playerID)
	isOwner := g.IsOwner(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	showStart := hasPlayer && isOwner && snap.Status == gamecommon.StatusLobby && len(g.Players) >= MinPlayers
	vm := snapToVM(snap, showStart, len(g.Players), playerName)

	renderFragment(w, r.Context(), drawviews.LobbyFragment(vm, gameID))
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
		log.Printf("[draw] start: no player cookie for game %s", gameID)
		http.Error(w, "not a player", http.StatusForbidden)
		return
	}
	if !g.IsOwner(playerID) {
		log.Printf("[draw] start: player %s is not owner of game %s", playerID, gameID)
		http.Error(w, "not the owner", http.StatusForbidden)
		return
	}
	if err := g.Start(time.Now().UTC()); err != nil {
		log.Printf("[draw] start: %v", err)
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.store.EnsureRoundLoop(gameID, g)
	h.store.Publish(gameID, "lobby")
	h.store.Publish(gameID, "round")
	h.store.Publish(gameID, "canvas")
	h.store.Publish(gameID, "wordhint")
	h.store.Publish(gameID, "players")
	h.store.Publish(gameID, "scores")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	playerName, _ := g.PlayerName(playerID)
	hub := h.store.Broadcaster(gameID)

	onEvent := func(w http.ResponseWriter, ctx context.Context, event string) {
		snap := g.Snapshot(time.Now().UTC(), playerID)
		showStart := playerID != "" && g.IsOwner(playerID) && snap.Status == gamecommon.StatusLobby && len(snap.Players) >= MinPlayers
		vm := snapToVM(snap, showStart, len(snap.Players), playerName)

		sendOne := func(name string, html string) { httputil.WriteSSE(w, name, html) }

		switch event {
		case "initial":
			lobbyHTML := ""
			if snap.Status == gamecommon.StatusLobby {
				lobbyHTML = renderComponent(ctx, drawviews.LobbyFragment(vm, gameID))
			}
			sendOne("lobby", lobbyHTML)
			sendOne("round", renderComponent(ctx, drawviews.RoundFragment(vm)))
			vmCanvas := vm
			vmCanvas.StrokesJSON = mustMarshalStrokes(vm.Strokes)
			sendOne("canvas", renderComponent(ctx, drawviews.CanvasFragment(vmCanvas, gameID)))
			sendOne("wordhint", renderComponent(ctx, drawviews.WordHintFragment(vm, gameID)))
			sendOne("players", renderComponent(ctx, drawviews.PlayersFragment(vm, playerID)))
			sendOne("scores", renderComponent(ctx, drawviews.ScoresFragment(vm)))
		case "lobby":
			lobbyHTML := ""
			if snap.Status == gamecommon.StatusLobby {
				lobbyHTML = renderComponent(ctx, drawviews.LobbyFragment(vm, gameID))
			}
			sendOne("lobby", lobbyHTML)
		case "round":
			sendOne("round", renderComponent(ctx, drawviews.RoundFragment(vm)))
		case "canvas":
			vmCanvas := vm
			vmCanvas.StrokesJSON = mustMarshalStrokes(vm.Strokes)
			sendOne("canvas", renderComponent(ctx, drawviews.CanvasFragment(vmCanvas, gameID)))
		case "wordhint":
			sendOne("wordhint", renderComponent(ctx, drawviews.WordHintFragment(vm, gameID)))
		case "players":
			sendOne("players", renderComponent(ctx, drawviews.PlayersFragment(vm, playerID)))
		case "scores":
			sendOne("scores", renderComponent(ctx, drawviews.ScoresFragment(vm)))
		}
	}

	if err := httputil.SSEStream(w, r, hub, onEvent); err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
	}
}

func (h *Handler) roundFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), drawviews.RoundFragment(snapToVM(snap, false, 0, pname)))
}

func (h *Handler) canvasFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	vm := snapToVM(snap, false, 0, pname)
	vm.StrokesJSON = mustMarshalStrokes(vm.Strokes)
	renderFragment(w, r.Context(), drawviews.CanvasFragment(vm, gameID))
}

func (h *Handler) wordHintFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), drawviews.WordHintFragment(snapToVM(snap, false, 0, pname), gameID))
}

func (h *Handler) playersFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), drawviews.PlayersFragment(snapToVM(snap, false, 0, pname), playerID))
}

func (h *Handler) scoresFragment(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	pname, _ := g.PlayerName(playerID)
	snap := g.Snapshot(time.Now().UTC(), playerID)
	renderFragment(w, r.Context(), drawviews.ScoresFragment(snapToVM(snap, false, 0, pname)))
}

func (h *Handler) updateCanvas(w http.ResponseWriter, r *http.Request, g *Game, playerID string) {
	gameID := g.ID
	if playerID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var strokes []DrawStroke
	if err := json.NewDecoder(r.Body).Decode(&strokes); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if g.UpdateCanvas(playerID, strokes) {
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

func renderPage(w http.ResponseWriter, ctx context.Context, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(ctx, w); err != nil {
		log.Printf("renderPage: %v", err)
	}
}

func renderFragment(w http.ResponseWriter, ctx context.Context, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(ctx, w); err != nil {
		log.Printf("renderFragment: %v", err)
	}
}

func renderComponent(ctx context.Context, c templ.Component) string {
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		log.Printf("renderComponent: %v", err)
	}
	return buf.String()
}

func mustMarshalStrokes(strokes []viewmodel.DrawStrokeView) string {
	b, err := json.Marshal(strokes)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func snapToVM(snap Snapshot, showStart bool, playerCount int, currentPlayerName string) viewmodel.SnapData {
	players := make([]viewmodel.PlayerInfo, len(snap.Players))
	for i, p := range snap.Players {
		players[i] = viewmodel.PlayerInfo{ID: p.ID, Name: p.Name, IsExplainer: p.IsExplainer}
	}
	scores := make([]viewmodel.ScoreEntry, len(snap.Scores))
	for i, s := range snap.Scores {
		scores[i] = viewmodel.ScoreEntry{Name: s.Name, Points: s.Points}
	}
	strokes := make([]viewmodel.DrawStrokeView, len(snap.Canvas))
	for i, st := range snap.Canvas {
		pts := make([]viewmodel.Point, len(st.Points))
		for j, p := range st.Points {
			pts[j] = viewmodel.Point{X: p.X, Y: p.Y}
		}
		color := st.Color
		if color == "" {
			color = "#000000"
		}
		strokes[i] = viewmodel.DrawStrokeView{Points: pts, Color: color}
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
		Strokes:          strokes,
		Players:          players,
		Scores:           scores,
		ShowStart:        showStart,
		PlayerCount:      playerCount,
		MinPlayers:       MinPlayers,
		CurrentPlayerName: currentPlayerName,
	}
}
