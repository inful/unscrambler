package words

import (
	"io/fs"
	"sort"
	"strings"
)

// LoadWords reads the word list for lang from sys (file at dir/lang.txt).
// Lang is trimmed and defaults to "en" if empty. Words are lowercased and
// trimmed; only words with len >= minLen are returned.
func LoadWords(sys fs.FS, dir, lang string, minLen int) ([]string, error) {
	name := strings.TrimSpace(lang)
	if name == "" {
		name = "en"
	}
	// Use forward slash: fs.FS (e.g. embed) uses "/" as separator on all platforms.
	fpath := dir + "/" + name + ".txt"
	b, err := fs.ReadFile(sys, fpath)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		w := strings.TrimSpace(strings.ToLower(line))
		if len(w) >= minLen {
			out = append(out, w)
		}
	}
	return out, nil
}

// SupportedLanguages returns language codes that have a .txt file in dir,
// sorted. Each code is the stem of the filename (e.g. "en" for "en.txt").
func SupportedLanguages(sys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(sys, dir)
	if err != nil {
		return nil, err
	}
	var codes []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".txt") {
			codes = append(codes, strings.TrimSuffix(name, ".txt"))
		}
	}
	sort.Strings(codes)
	return codes, nil
}
