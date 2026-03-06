package id

import (
	"strings"
	"testing"
)

func TestNewID_NotEmpty(t *testing.T) {
	got := NewID()
	if got == "" {
		t.Error("NewID() must not return empty string")
	}
}

func TestNewID_LengthAndChars(t *testing.T) {
	got := NewID()
	// 10 bytes -> 16 base32 chars (no padding)
	if len(got) != 16 {
		t.Errorf("NewID() len = %d, want 16", len(got))
	}
	valid := "abcdefghijklmnopqrstuvwxyz234567"
	for _, c := range got {
		if !strings.ContainsRune(valid, c) {
			t.Errorf("NewID() contains invalid char %q", c)
		}
	}
	if got != strings.ToLower(got) {
		t.Error("NewID() must be lowercase")
	}
}

func TestNewID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s := NewID()
		if seen[s] {
			t.Errorf("duplicate ID %q at iteration %d", s, i)
		}
		seen[s] = true
	}
}
