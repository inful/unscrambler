package words

import (
	"testing"
	"testing/fstest"
)

var testFS = fstest.MapFS{
	"words/en.txt": {Data: []byte("hello\nWORLD\n  hi  \nabc\nabcd\nabcde\nlonger\n")},
	"words/no.txt": {Data: []byte("hei\nverden\n")},
}

const testDir = "words"

func TestLoadWords_ValidLang(t *testing.T) {
	got, err := LoadWords(testFS, testDir, "en", 5)
	if err != nil {
		t.Fatalf("LoadWords(en, 5): %v", err)
	}
	want := []string{"hello", "world", "abcde", "longer"}
	if len(got) != len(want) {
		t.Errorf("LoadWords: got %d words, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("LoadWords: at %d got %q, want %q", i, safeAt(got, i), w)
		}
	}
}

func TestLoadWords_LowercaseAndTrimmed(t *testing.T) {
	got, err := LoadWords(testFS, testDir, "en", 3)
	if err != nil {
		t.Fatalf("LoadWords: %v", err)
	}
	for _, w := range got {
		if w != toLower(w) {
			t.Errorf("word %q should be lowercased", w)
		}
	}
	// minLen 2 so "  hi  " is included after trim -> "hi"
	got2, _ := LoadWords(testFS, testDir, "en", 2)
	found := false
	for _, w := range got2 {
		if w == "hi" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'hi' in results for minLen 2: %v", got2)
	}
}

func TestLoadWords_EmptyLangDefaultsToEn(t *testing.T) {
	got, err := LoadWords(testFS, testDir, "", 5)
	if err != nil {
		t.Fatalf("LoadWords(empty, 5): %v", err)
	}
	if len(got) == 0 {
		t.Error("LoadWords(empty) should return en words")
	}
}

func TestLoadWords_WhitespaceLangDefaultsToEn(t *testing.T) {
	got, err := LoadWords(testFS, testDir, "  ", 5)
	if err != nil {
		t.Fatalf("LoadWords(whitespace, 5): %v", err)
	}
	if len(got) == 0 {
		t.Error("LoadWords(whitespace) should return en words")
	}
}

func TestLoadWords_MissingFileReturnsError(t *testing.T) {
	_, err := LoadWords(testFS, testDir, "de", 5)
	if err == nil {
		t.Error("LoadWords(de): expected error for missing file")
	}
}

func TestLoadWords_MinLenFiltersShortWords(t *testing.T) {
	got, err := LoadWords(testFS, testDir, "en", 6)
	if err != nil {
		t.Fatalf("LoadWords: %v", err)
	}
	for _, w := range got {
		if len(w) < 6 {
			t.Errorf("word %q has len < 6", w)
		}
	}
	// Only "longer" has len >= 6 in test data (hello=5, world=5, etc.)
	if len(got) != 1 || got[0] != "longer" {
		t.Errorf("expected [longer], got %v", got)
	}
}

func TestLoadWords_DirPrefix(t *testing.T) {
	// LoadWords uses "words/" + lang + ".txt"
	got, err := LoadWords(testFS, testDir, "no", 2)
	if err != nil {
		t.Fatalf("LoadWords(no): %v", err)
	}
	want := []string{"hei", "verden"}
	if len(got) != len(want) {
		t.Errorf("LoadWords(no): got %v, want %v", got, want)
	}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Errorf("at %d: got %q, want %q", i, safeAt(got, i), w)
		}
	}
}

func TestSupportedLanguages(t *testing.T) {
	got, err := SupportedLanguages(testFS, testDir)
	if err != nil {
		t.Fatalf("SupportedLanguages: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d languages, want 2: %v", len(got), got)
	}
	// Should be sorted and without .txt
	hasEn := false
	hasNo := false
	for _, c := range got {
		if c == "en" {
			hasEn = true
		}
		if c == "no" {
			hasNo = true
		}
	}
	if !hasEn || !hasNo {
		t.Errorf("SupportedLanguages: want en and no, got %v", got)
	}
}

func safeAt(s []string, i int) string {
	if i >= len(s) {
		return "<out of range>"
	}
	return s[i]
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
