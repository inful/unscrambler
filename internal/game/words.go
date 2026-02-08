package game

import (
	"embed"
	"io/fs"
	"math/rand"
	"strings"
	"time"
)

//go:embed words/*.txt
var wordsFS embed.FS

const minWordLen = 6

// SupportedLanguages returns language codes that have an embedded word list.
func SupportedLanguages() []string {
	return []string{"en", "no"}
}

// loadWords reads the embedded word file for lang and returns words of at least minWordLen.
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

// BuildRounds builds count rounds for the given language, shuffling words and letters.
func BuildRounds(lang string, count int) []Round {
	if count < 1 {
		count = 1
	}
	pool, err := loadWords(lang)
	if err != nil || len(pool) == 0 {
		pool, _ = loadWords("en")
	}
	if len(pool) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	rounds := make([]Round, 0, count)
	for i := 0; i < count; i++ {
		word := pool[i%len(pool)]
		rounds = append(rounds, Round{
			Word:      word,
			Scrambled: scrambleWord(word, rng),
		})
	}
	return rounds
}

func scrambleWord(word string, rng *rand.Rand) string {
	letters := strings.Split(word, "")
	rng.Shuffle(len(letters), func(i, j int) {
		letters[i], letters[j] = letters[j], letters[i]
	})
	return strings.Join(letters, "")
}
