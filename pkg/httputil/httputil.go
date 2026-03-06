package httputil

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

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
