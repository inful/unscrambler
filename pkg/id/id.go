package id

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// NewID returns a short, URL-safe random string (16 chars, base32-encoded).
// Suitable for game and player IDs.
func NewID() string {
	buf := make([]byte, 10)
	_, _ = rand.Read(buf)
	encoder := base32.StdEncoding.WithPadding(base32.NoPadding)
	return strings.ToLower(encoder.EncodeToString(buf))
}
