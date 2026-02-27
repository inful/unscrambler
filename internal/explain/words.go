package explain

import (
	"embed"
	"io/fs"
	"math/rand"
	"strings"
)

//go:embed words/*.txt
var wordsFS embed.FS

const minWordLen = 5

func loadWords(lang string) ([]string, error) {
	name := strings.TrimSpace(lang)
	if name == "" {
		name = "en"
	}
	name = "words/" + name + ".txt"
	b, err := fs.ReadFile(wordsFS, name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		w := strings.TrimSpace(strings.ToLower(line))
		if len(w) >= minWordLen {
			out = append(out, w)
		}
	}
	return out, nil
}

// PickRandomWord returns a random word for the given language.
func PickRandomWord(lang string, rng *rand.Rand) string {
	pool, err := loadWords(lang)
	if err != nil || len(pool) == 0 {
		pool, _ = loadWords("en")
	}
	if len(pool) == 0 {
		return ""
	}
	return pool[rng.Intn(len(pool))]
}

// SupportedLanguages returns language codes that have an embedded word list.
func SupportedLanguages() []string {
	return []string{"en"}
}
