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
