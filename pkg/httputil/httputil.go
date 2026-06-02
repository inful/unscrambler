package httputil

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxUsernameLen = 20

// ParseJoinForm parses the request form and returns a validated username.
// It returns an error if the form is invalid or username is empty after trim.
// Username is trimmed and truncated to maxUsernameLen (20) characters.
func ParseJoinForm(r *http.Request) (username string, err error) {
	if err := r.ParseForm(); err != nil {
		return "", err
	}
	username = strings.TrimSpace(r.FormValue("username"))
	if username == "" {
		return "", errors.New("username required")
	}
	if len(username) > maxUsernameLen {
		username = username[:maxUsernameLen]
	}
	return username, nil
}

// JoinParams holds parameters for HandleJoin.
type JoinParams struct {
	GameID       string
	CookieName   func(gameID string) string
	AddPlayer    func(username string) (playerID string)
	Publish      func(gameID, event string)
	RedirectPath string
	// AfterPublish is called after Publish(gameID, "players"), before redirect (e.g. publish more events).
	AfterPublish func()
}

// HandleJoin parses the join form, adds the player, sets the cookie, publishes
// "players", and redirects to RedirectPath. On parse/validation error returns 400.
func HandleJoin(w http.ResponseWriter, r *http.Request, p JoinParams) {
	username, err := ParseJoinForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	playerID := p.AddPlayer(username)
	SetPlayerCookie(w, p.CookieName(p.GameID), playerID)
	p.Publish(p.GameID, "players")
	if p.AfterPublish != nil {
		p.AfterPublish()
	}
	http.Redirect(w, r, p.RedirectPath, http.StatusSeeOther)
}

// StartParams holds parameters for HandleStartGame.
type StartParams struct {
	GameID     string
	PlayerID   string
	IsOwner    func(gameID, playerID string) bool
	Start      func() error
	AfterStart func()
	// NoRedirect: if true, write 204 on success (for fetch/HTMX); otherwise redirect to game page.
	NoRedirect bool
}

// HandleStartGame checks that the player is the owner, calls Start(), then
// AfterStart() (e.g. ensure round loop + publish). If not owner, redirects to
// game page. On success: if NoRedirect, writes 204; otherwise redirects.
func HandleStartGame(w http.ResponseWriter, r *http.Request, p StartParams) {
	if p.PlayerID == "" || !p.IsOwner(p.GameID, p.PlayerID) {
		http.Redirect(w, r, "/game/"+p.GameID, http.StatusSeeOther)
		return
	}
	if err := p.Start(); err != nil {
		// Already started (e.g. double-submit or two tabs) → redirect to game page
		if strings.Contains(err.Error(), "already started") {
			http.Redirect(w, r, "/game/"+p.GameID, http.StatusSeeOther)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.AfterStart()
	if p.NoRedirect {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/game/"+p.GameID, http.StatusSeeOther)
}

// ErrStreamingUnsupported is returned by SSEStream when the response writer
// does not implement http.Flusher.
var ErrStreamingUnsupported = errors.New("streaming unsupported")

// ParseInt parses s as an integer. If s is empty or invalid, fallback is returned.
func ParseInt(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

// WriteSSE writes a Server-Sent Events message: event name, then data (each line
// prefixed with "data: "), then a blank line.
func WriteSSE(w http.ResponseWriter, event, data string) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	for _, line := range strings.Split(data, "\n") {
		_, _ = w.Write([]byte("data: " + line + "\n"))
	}
	_, _ = w.Write([]byte("\n"))
}

// BuildInviteURL returns the full URL for a game page. If BASE_URL is set it is
// used (trimmed and without trailing slash); otherwise scheme and host from r
// are used. pathPrefix is joined with gameID to form the path (e.g. "/game/" + gameID).
func BuildInviteURL(r *http.Request, pathPrefix, gameID string) string {
	if base := strings.TrimSpace(os.Getenv("BASE_URL")); base != "" {
		return strings.TrimRight(base, "/") + pathPrefix + gameID
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + pathPrefix + gameID
}

// GetPlayerIDFromCookie returns the value of the cookie with the given name, or
// empty string if the cookie is missing.
func GetPlayerIDFromCookie(r *http.Request, cookieName string) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetPlayerCookie sets a cookie with the given name and value (e.g. player ID).
// Path is "/", HttpOnly and SameSite=Lax; expiry is 24 hours.
func SetPlayerCookie(w http.ResponseWriter, cookieName, playerID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    playerID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})
}

// GameGetter is implemented by stores that can look up a game by ID.
type GameGetter[G any] interface {
	GetGame(id string) (G, bool)
}

// GameHandler is the signature for handlers that receive the resolved game and player ID.
type GameHandler[G any] func(w http.ResponseWriter, r *http.Request, g G, playerID string)

// WithGame returns a middleware that resolves the game and player ID from the request,
// returns 404 if the game is missing, then calls next with (w, r, game, playerID).
// getGameID extracts the game ID from r (e.g. from the route, such as chi.URLParam(r, "id")).
func WithGame[G any](store GameGetter[G], cookieName func(gameID string) string, getGameID func(*http.Request) string) func(next GameHandler[G]) http.HandlerFunc {
	return func(next GameHandler[G]) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			gameID := getGameID(r)
			if gameID == "" {
				http.NotFound(w, r)
				return
			}
			g, ok := store.GetGame(gameID)
			if !ok {
				http.NotFound(w, r)
				return
			}
			playerID := GetPlayerIDFromCookie(r, cookieName(gameID))
			next(w, r, g, playerID)
		}
	}
}

// EventHub is implemented by broadcasters that deliver event names to subscribers
// (e.g. for Server-Sent Events). *realtime.Broadcaster satisfies this interface.
type EventHub interface {
	Subscribe() chan string
	Unsubscribe(chan string)
}

// SSEHandler is called by SSEStream for each event. It should write one or more
// SSE messages to w using WriteSSE. The event is "initial" for the first send,
// then the event name received from the hub (e.g. "round", "scores").
type SSEHandler func(w http.ResponseWriter, ctx context.Context, event string)

// SSEStream runs an SSE loop: sets headers, subscribes to hub, calls onEvent for
// "initial" then for each event from the hub, and sends a keepalive comment every
// 25 seconds. Returns ErrStreamingUnsupported if w does not implement http.Flusher.
func SSEStream(w http.ResponseWriter, r *http.Request, hub EventHub, onEvent SSEHandler) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return ErrStreamingUnsupported
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	ctx := r.Context()
	onEvent(w, ctx, "initial")
	flusher.Flush()

	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event := <-sub:
			onEvent(w, ctx, event)
			flusher.Flush()
		case <-keepAlive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}
