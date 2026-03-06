package explain

import (
	"embed"
	"math/rand"

	"dagame/pkg/words"
)

//go:embed words/*.txt
var wordsFS embed.FS

const minWordLen = 5

func loadWords(lang string) ([]string, error) {
	return words.LoadWords(wordsFS, "words", lang, minWordLen)
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
	langs, _ := words.SupportedLanguages(wordsFS, "words")
	return langs
}
