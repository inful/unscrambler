package httputil

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseInt(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		fallback int
		want     int
	}{
		{"empty string returns fallback", "", 42, 42},
		{"valid number", "7", 0, 7},
		{"zero", "0", 99, 0},
		{"negative", "-3", 0, -3},
		{"invalid returns fallback", "abc", 10, 10},
		{"partial number returns fallback", "12x", 5, 5},
		{"whitespace only is invalid", "  ", 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInt(tt.s, tt.fallback)
			if got != tt.want {
				t.Errorf("ParseInt(%q, %d) = %d, want %d", tt.s, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestWriteSSE(t *testing.T) {
	w := httptest.NewRecorder()
	event := "round"
	data := "line1\nline2"

	WriteSSE(w, event, data)

	body := w.Body.String()
	if !strings.Contains(body, "event: round\n") {
		t.Errorf("body missing event line: %q", body)
	}
	if !strings.Contains(body, "data: line1\n") {
		t.Errorf("body missing data line1: %q", body)
	}
	if !strings.Contains(body, "data: line2\n") {
		t.Errorf("body missing data line2: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("body should end with double newline, got: %q", body)
	}
}

func TestWriteSSE_EmptyData(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSSE(w, "ping", "")

	body := w.Body.String()
	if !strings.Contains(body, "event: ping\n") {
		t.Errorf("body missing event: %q", body)
	}
	// Empty string split by "\n" gives one empty element, so one "data: \n"
	if !strings.Contains(body, "data: ") {
		t.Errorf("body should contain data line: %q", body)
	}
}

func TestBuildInviteURL_UseBaseURLWhenSet(t *testing.T) {
	os.Setenv("BASE_URL", "https://example.com ")
	defer func() { _ = os.Unsetenv("BASE_URL") }()

	r := httptest.NewRequest(http.MethodGet, "http://ignored/", nil)
	got := BuildInviteURL(r, "/game/", "abc123")

	want := "https://example.com/game/abc123"
	if got != want {
		t.Errorf("BuildInviteURL() = %q, want %q", got, want)
	}
}

func TestBuildInviteURL_TrimTrailingSlashFromBase(t *testing.T) {
	os.Setenv("BASE_URL", "https://example.com/")
	defer func() { _ = os.Unsetenv("BASE_URL") }()

	r := httptest.NewRequest(http.MethodGet, "http://ignored/", nil)
	got := BuildInviteURL(r, "/game/", "xyz")

	want := "https://example.com/game/xyz"
	if got != want {
		t.Errorf("BuildInviteURL() = %q, want %q", got, want)
	}
}

func TestBuildInviteURL_UseRequestHostWhenNoBaseURL(t *testing.T) {
	_ = os.Unsetenv("BASE_URL")

	r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	got := BuildInviteURL(r, "/game/", "game1")

	want := "http://localhost:8080/game/game1"
	if got != want {
		t.Errorf("BuildInviteURL() = %q, want %q", got, want)
	}
}

func TestBuildInviteURL_UseHTTPSWhenTLS(t *testing.T) {
	_ = os.Unsetenv("BASE_URL")

	r := httptest.NewRequest(http.MethodGet, "https://localhost:8443/", nil)
	r.TLS = &tls.ConnectionState{}
	got := BuildInviteURL(r, "/game/", "id")

	want := "https://localhost:8443/game/id"
	if got != want {
		t.Errorf("BuildInviteURL() = %q, want %q", got, want)
	}
}

func TestGetPlayerIDFromCookie_Missing(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := GetPlayerIDFromCookie(r, "player_abc")
	if got != "" {
		t.Errorf("GetPlayerIDFromCookie() = %q, want empty", got)
	}
}

func TestGetPlayerIDFromCookie_Present(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "player_abc", Value: "player-id-123"})

	got := GetPlayerIDFromCookie(r, "player_abc")
	want := "player-id-123"
	if got != want {
		t.Errorf("GetPlayerIDFromCookie() = %q, want %q", got, want)
	}
}

func TestSetPlayerCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetPlayerCookie(w, "player_game1", "pid-456")

	res := w.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "player_game1" {
		t.Errorf("cookie Name = %q, want player_game1", c.Name)
	}
	if c.Value != "pid-456" {
		t.Errorf("cookie Value = %q, want pid-456", c.Value)
	}
	if c.Path != "/" {
		t.Errorf("cookie Path = %q, want /", c.Path)
	}
	if !c.HttpOnly {
		t.Error("cookie should be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite = %v, want Lax", c.SameSite)
	}
	if c.Expires.IsZero() {
		t.Error("cookie Expires should be set (e.g. 24h)")
	}
}
