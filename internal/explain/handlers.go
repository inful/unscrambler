package explain

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
	// Simple HTML for create game form
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	html := `<!DOCTYPE html>
<html>
<head><title>Explain</title><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css"></head>
<body>
<section class="section">
<div class="container">
<h1 class="title">Explain</h1>
<p class="subtitle">One player explains a word with emojis; others guess. Realtime canvas.</p>
<form method="POST" action="/games" class="box">
	<div class="field">
		<label class="label">Rounds</label>
		<div class="control"><input class="input" type="number" name="rounds" value="3" min="1" max="10"></div>
	</div>
	<div class="field">
		<label class="label">Round duration (seconds)</label>
		<div class="control"><input class="input" type="number" name="duration" value="90" min="30" max="300"></div>
	</div>
	<div class="field">
		<label class="label">Emojis per round</label>
		<div class="control"><input class="input" type="number" name="emojis" value="8" min="4" max="20"></div>
	</div>
	<div class="field">
		<div class="control"><button type="submit" class="button is-primary">Create game</button></div>
	</div>
</form>
</div>
</section>
</body>
</html>`
	_, _ = w.Write([]byte(html))
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
	inviteURL := buildInviteURL(r, gameID)
	isOwner := g.IsOwner(playerID)
	showStart := hasPlayer && isOwner && snap.Status == StatusLobby && len(g.Players) >= MinPlayers

	data := map[string]interface{}{
		"GameID":           gameID,
		"InviteURL":        inviteURL,
		"Snapshot":         snap,
		"HasPlayer":        hasPlayer,
		"PlayerName":       playerName,
		"PlayerID":         playerID,
		"IsOwner":          isOwner,
		"ShowStart":        showStart,
		"MinPlayers":       MinPlayers,
		"PlayerCount":      len(g.Players),
		"RoundDurationSec": int(snap.RoundDuration.Seconds()),
	}
	renderGamePage(w, data)
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderLobbyFragmentHTML(showStart, gameID, len(g.Players))))
}

// renderLobbyFragmentHTML returns the HTML for #lobby-actions.
// Start is a non-navigating fetch POST; the SSE stream delivers the game-start events that update the page in place.
// This avoids ALL navigation/redirect, which consistently hangs in this setup.
func renderLobbyFragmentHTML(showStart bool, gameID string, playerCount int) string {
	if showStart {
		return `<button type="button" class="button is-primary mb-4" onclick="fetch('/game/` + gameID + `/start',{method:'POST'})">Start game</button>`
	}
	return `<p class="mb-4">Waiting for more players (min ` + strconv.Itoa(MinPlayers) + `). Current: ` + strconv.Itoa(playerCount) + `.</p>`
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

	sendAll := func() {
		snap := g.Snapshot(time.Now().UTC(), playerID)
		lobbyHTML := ""
		if snap.Status == StatusLobby {
			showStart := playerID != "" && g.IsOwner(playerID) && len(snap.Players) >= MinPlayers
			lobbyHTML = renderLobbyFragmentHTML(showStart, gameID, len(snap.Players))
		}
		writeSSE(w, "lobby", lobbyHTML)
		writeSSE(w, "round", renderRoundFragmentHTML(snap, gameID))
		writeSSE(w, "canvas", renderCanvasFragmentHTML(snap))
		writeSSE(w, "wordhint", renderWordHintFragmentHTML(snap, gameID))
		writeSSE(w, "players", renderPlayersFragmentHTML(snap, playerID))
		writeSSE(w, "scores", renderScoresFragmentHTML(snap))
		flusher.Flush()
	}
	sendAll()

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-sub:
			snap := g.Snapshot(time.Now().UTC(), playerID)
			switch event {
			case "lobby":
				lobbyHTML := ""
				if snap.Status == StatusLobby {
					showStart := playerID != "" && g.IsOwner(playerID) && len(snap.Players) >= MinPlayers
					lobbyHTML = renderLobbyFragmentHTML(showStart, gameID, len(snap.Players))
				}
				writeSSE(w, "lobby", lobbyHTML)
			case "round":
				writeSSE(w, "round", renderRoundFragmentHTML(snap, gameID))
			case "canvas":
				writeSSE(w, "canvas", renderCanvasFragmentHTML(snap))
			case "wordhint":
				writeSSE(w, "wordhint", renderWordHintFragmentHTML(snap, gameID))
			case "players":
				writeSSE(w, "players", renderPlayersFragmentHTML(snap, playerID))
			case "scores":
				writeSSE(w, "scores", renderScoresFragmentHTML(snap))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderRoundFragmentHTML(snap, gameID)))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderCanvasFragmentHTML(snap)))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderWordHintFragmentHTML(snap, gameID)))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderPlayersFragmentHTML(snap, playerID)))
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderScoresFragmentHTML(snap)))
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

// Stub render functions - return HTML strings. Can be replaced with templ later.
func renderGamePage(w http.ResponseWriter, data map[string]interface{}) {
	snap, _ := data["Snapshot"].(Snapshot)
	gameID, _ := data["GameID"].(string)
	inviteURL, _ := data["InviteURL"].(string)
	hasPlayer := data["HasPlayer"].(bool)
	playerName, _ := data["PlayerName"].(string)
	playerID, _ := data["PlayerID"].(string)
	showStart := data["ShowStart"].(bool)
	playerCount := 0
	if n, ok := data["PlayerCount"].(int); ok {
		playerCount = n
	}

	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html><html><head><title>Explain</title>`)
	buf.WriteString(`<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css">`)
	buf.WriteString(`</head><body class="section">`)
	buf.WriteString(`<div class="container"><h1 class="title">Explain</h1>`)

	if !hasPlayer {
		// Not joined yet: show join form only.
		buf.WriteString(`<form method="POST" action="/game/` + gameID + `/join" class="box">`)
		buf.WriteString(`<div class="field"><label class="label">Your name</label><div class="control"><input class="input" name="username" required></div></div>`)
		buf.WriteString(`<button type="submit" class="button is-primary">Join</button></form>`)
		buf.WriteString(`<p class="help">Invite: ` + inviteURL + `</p>`)
	} else {
		// Joined: show game sections and open SSE stream — same pattern as cmd/web.
		// Start is a non-navigating fetch POST so there is no navigation that could hang.
		// The SSE stream delivers round/canvas/wordhint/players/scores events that update the page in place.
		buf.WriteString(`<p>Hello, ` + playerName + `</p>`)
		buf.WriteString(`<p class="help">Invite: ` + inviteURL + `</p>`)

		// Lobby-actions: shown in lobby, emptied by "lobby" SSE event when game starts.
		buf.WriteString(`<div id="lobby-actions">`)
		buf.WriteString(renderLobbyFragmentHTML(showStart, gameID, playerCount))
		buf.WriteString(`</div>`)

		// Game sections are always present; initial content comes from snapshot, then SSE updates them.
		buf.WriteString(`<div id="round">`)
		buf.WriteString(renderRoundFragmentHTML(snap, gameID))
		buf.WriteString(`</div>`)
		buf.WriteString(`<div id="canvas">`)
		buf.WriteString(renderCanvasFragmentHTML(snap))
		buf.WriteString(`</div>`)
		buf.WriteString(`<div id="wordhint">`)
		buf.WriteString(renderWordHintFragmentHTML(snap, gameID))
		buf.WriteString(`</div>`)
		buf.WriteString(`<div id="players">`)
		buf.WriteString(renderPlayersFragmentHTML(snap, playerID))
		buf.WriteString(`</div>`)
		buf.WriteString(`<div id="scores">`)
		buf.WriteString(renderScoresFragmentHTML(snap))
		buf.WriteString(`</div>`)

		// Single script block: SSE + canvas drag-and-drop interaction.
		buf.WriteString(`<script>
(function(){
var gid="` + gameID + `";
var _dragging=false; // suppress SSE canvas updates while dragging to avoid flicker

// SSE: update sections in place.
var src=new EventSource("/game/"+gid+"/stream");
src.addEventListener("lobby",   function(e){ var el=document.getElementById("lobby-actions"); if(el) el.innerHTML=e.data; });
src.addEventListener("round",   function(e){ var el=document.getElementById("round");         if(el) el.innerHTML=e.data; });
src.addEventListener("canvas",  function(e){ if(_dragging) return; var el=document.getElementById("canvas"); if(el) el.innerHTML=e.data; });
src.addEventListener("wordhint",function(e){ var el=document.getElementById("wordhint");      if(el) el.innerHTML=e.data; });
src.addEventListener("players", function(e){ var el=document.getElementById("players");       if(el) el.innerHTML=e.data; });
src.addEventListener("scores",  function(e){ var el=document.getElementById("scores");        if(el) el.innerHTML=e.data; });

function collectItems(area){
  var items=[];
  area.querySelectorAll(".canvas-emoji").forEach(function(el){
    items.push({ID:el.dataset.id,Emoji:el.dataset.emoji,X:parseFloat(el.style.left)||0,Y:parseFloat(el.style.top)||0});
  });
  return items;
}
function saveCanvas(items){
  fetch("/game/"+gid+"/canvas",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(items)});
}

// ── Drag palette emoji onto canvas (HTML5 drag-and-drop) ─────────────────────
document.body.addEventListener("dragstart",function(e){
  var btn=e.target.closest(".emoji-btn");
  if(!btn) return;
  e.dataTransfer.setData("text/plain",btn.dataset.emoji);
  e.dataTransfer.effectAllowed="copy";
});
document.body.addEventListener("dragover",function(e){
  if(e.target.closest("#canvas-area")){ e.preventDefault(); e.dataTransfer.dropEffect="copy"; }
});
document.body.addEventListener("drop",function(e){
  var area=e.target.closest("#canvas-area");
  if(!area) return;
  var emoji=e.dataTransfer.getData("text/plain");
  if(!emoji) return;
  e.preventDefault();
  var rect=area.getBoundingClientRect();
  var x=e.clientX-rect.left-16;
  var y=e.clientY-rect.top-16;
  var id="e"+Date.now()+"-"+Math.random().toString(36).slice(2);
  // Optimistic local update
  var span=document.createElement("span");
  span.className="canvas-emoji";
  span.dataset.id=id; span.dataset.emoji=emoji;
  span.style.cssText="position:absolute;left:"+x+"px;top:"+y+"px;font-size:2rem;cursor:grab;user-select:none;";
  span.textContent=emoji;
  area.appendChild(span);
  var items=collectItems(area);
  saveCanvas(items);
});

// ── Drag existing canvas emoji to reposition (mouse events) ──────────────────
var _drag=null;
document.body.addEventListener("mousedown",function(e){
  var el=e.target.closest(".canvas-emoji");
  if(!el) return;
  var area=el.closest("#canvas-area");
  if(!area) return;
  e.preventDefault();
  _dragging=true;
  var er=el.getBoundingClientRect();
  _drag={el:el,area:area,ox:e.clientX-er.left,oy:e.clientY-er.top};
  el.style.zIndex="100"; el.style.cursor="grabbing";
});
document.addEventListener("mousemove",function(e){
  if(!_drag) return;
  var ar=_drag.area.getBoundingClientRect();
  _drag.el.style.left=(e.clientX-ar.left-_drag.ox)+"px";
  _drag.el.style.top =(e.clientY-ar.top -_drag.oy)+"px";
});
document.addEventListener("mouseup",function(){
  if(!_drag) return;
  _drag.el.style.zIndex=""; _drag.el.style.cursor="grab";
  var items=collectItems(_drag.area);
  _drag=null; _dragging=false;
  saveCanvas(items);
});
})();
</script>`)
	}
	buf.WriteString(`</div></body></html>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func renderRoundFragmentHTML(snap Snapshot, gameID string) string {
	var b bytes.Buffer
	if snap.Status == StatusLobby {
		b.WriteString(`<div class="notification is-info">Waiting to start.</div>`)
		return b.String()
	}
	b.WriteString(`<div class="box"><p>Round ` + strconv.Itoa(snap.CurrentRound) + `/` + strconv.Itoa(snap.Rounds) + `</p>`)
	b.WriteString(`<p>Duration: ` + strconv.Itoa(int(snap.RoundDuration.Seconds())) + `s</p>`)
	if snap.ExplainerName != "" {
		b.WriteString(`<p>Explainer: <strong>` + snap.ExplainerName + `</strong></p>`)
	}
	if snap.RoundWinnerName != "" {
		b.WriteString(`<p class="has-text-success">` + snap.RoundWinnerName + ` got it!</p>`)
	}
	if snap.Status == StatusFinished {
		b.WriteString(`<p class="title is-5">Game over. Winner: ` + snap.WinnerName + `</p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func renderCanvasFragmentHTML(snap Snapshot) string {
	var b bytes.Buffer
	b.WriteString(`<div class="box"><h3 class="subtitle">Canvas</h3><div id="canvas-area" class="mb-3" style="min-height:200px;border:1px solid #ccc;position:relative;overflow:hidden;">`)
	for _, item := range snap.Canvas {
		b.WriteString(`<span class="canvas-emoji" data-id="` + item.ID + `" data-emoji="` + item.Emoji + `" style="position:absolute;left:` + strconv.FormatFloat(item.X, 'f', 0, 64) + `px;top:` + strconv.FormatFloat(item.Y, 'f', 0, 64) + `px;font-size:2rem;cursor:grab;user-select:none;">` + item.Emoji + `</span>`)
	}
	b.WriteString(`</div>`)
	if snap.IsExplainer && snap.Status == StatusInProgress && snap.RoundWinnerName == "" {
		b.WriteString(`<div class="emoji-palette mb-2">`)
		for _, em := range snap.RoundEmojis {
			b.WriteString(`<button type="button" class="button is-small emoji-btn" draggable="true" data-emoji="` + em + `" style="cursor:grab;font-size:1.25rem;">` + em + `</button>`)
		}
		b.WriteString(`</div><p class="help">Drag emojis from the palette onto the canvas. Drag placed emojis to reposition.</p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func renderWordHintFragmentHTML(snap Snapshot, gameID string) string {
	var b bytes.Buffer
	b.WriteString(`<div class="box"><h3 class="subtitle">Word</h3><p class="is-size-4">`)
	if snap.IsExplainer {
		b.WriteString(`<strong>` + snap.Word + `</strong></p>`)
	} else {
		b.WriteString(`<span class="word-display">` + snap.RevealedWord + `</span> (` + strconv.Itoa(snap.WordLength) + ` letters)`)
		if snap.Status == StatusInProgress && snap.RoundWinnerName == "" {
			b.WriteString(`</p><form onsubmit="(function(f){var i=f.querySelector('input[name=guess]');var g=i.value.trim();if(!g)return;fetch('/game/` + gameID + `/guess',{method:'POST',body:new URLSearchParams({guess:g})}).then(function(){i.value='';i.focus();});f.returnValue=false;})(this);return false;">`)
			b.WriteString(`<div class="field has-addons"><div class="control"><input class="input" name="guess" placeholder="Your guess" autocomplete="off"></div><div class="control"><button type="submit" class="button is-primary">Guess</button></div></div></form>`)
		} else {
			b.WriteString(`</p>`)
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

func renderPlayersFragmentHTML(snap Snapshot, currentPlayerID string) string {
	var b bytes.Buffer
	b.WriteString(`<div class="box"><h3 class="subtitle">Players</h3><ul>`)
	for _, p := range snap.Players {
		role := "guesser"
		if p.IsExplainer {
			role = "explainer"
		}
		strong := ""
		if p.ID == currentPlayerID {
			strong = "<strong>"
		}
		b.WriteString(`<li>` + strong + p.Name + `</strong> (` + role + `)</li>`)
	}
	b.WriteString(`</ul></div>`)
	return b.String()
}

func renderScoresFragmentHTML(snap Snapshot) string {
	var b bytes.Buffer
	b.WriteString(`<div class="box"><h3 class="subtitle">Scores</h3><ol>`)
	for i, s := range snap.Scores {
		b.WriteString(`<li>` + s.Name + `: ` + strconv.Itoa(s.Points) + `</li>`)
		_ = i
	}
	b.WriteString(`</ol></div>`)
	return b.String()
}
