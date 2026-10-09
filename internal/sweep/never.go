package sweep

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// baked is the hashes file cmd/never writes from the list kept outside the
// repository. git ignores it, so a game built from a clone has no never-words
// and refuses to release.
//
//go:embed never
var baked embed.FS

// Never holds a salt and the hashes of the never-words. it holds no word.
type Never struct {
	salt   string
	hashes map[string]bool
}

// NeverWord is the kind of hit a never-word makes. it cannot say which one.
const NeverWord = "a never-word"

// longest is the most words a never-word may have, three. a longer entry is
// refused when baked.
const longest = 3

// Baked is the list of never-words, or empty if none.
func Baked() Never {
	body, err := baked.ReadFile("never/hashes")
	if err != nil {
		return Never{}
	}
	return ParseNever(string(body))
}

// Empty is true when there are no never-words.
func (never Never) Empty() bool {
	return len(never.hashes) == 0
}

// Bake is the hashes file for a list of never-words: the salt on the first
// line, then the sorted hashes, one per line. an entry is one to three words,
// matched without case or punctuation. any other entry is an error naming its
// place in the list.
func Bake(salt string, list []string) (string, error) {
	var hashes []string
	for place, entry := range list {
		run := wordsOf(entry)
		if len(run) == 0 || len(run) > longest {
			return "", fmt.Errorf(
				"never-word %d is %d words; a never-word "+
					"is a word or a name of two or three",
				place+1, len(run),
			)
		}
		hashes = append(hashes, hashOf(salt, strings.Join(run, " ")))
	}
	sort.Strings(hashes)
	return "salt " + salt + "\n" + strings.Join(hashes, "\n") + "\n", nil
}

// ParseNever reads a hashes file written by cmd/never.
func ParseNever(body string) Never {
	never := Never{hashes: map[string]bool{}}
	lines := strings.Split(body, "\n")
	never.salt = strings.TrimPrefix(strings.TrimSpace(lines[0]), "salt ")
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" {
			never.hashes[line] = true
		}
	}
	return never
}

// hashOf is the hex sha-256 of the salt, a nul, then the run. the run must be
// lower case, with one space between words.
func hashOf(salt, run string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + run))
	return hex.EncodeToString(sum[:])
}

// wordsOf is a text's words, lower case. a word is a run of letters and
// digits, and anything else splits.
func wordsOf(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(char rune) bool {
		return !unicode.IsLetter(char) && !unicode.IsDigit(char)
	})
}

// found checks if text contains a never-word.
func (never Never) found(text string) bool {
	if never.Empty() {
		return false
	}
	words := wordsOf(text)
	for i := range words {
		run := words[i]
		for k := 1; ; k++ {
			if never.hashes[hashOf(never.salt, run)] {
				return true
			}
			if k == longest || i+k == len(words) {
				break
			}
			run += " " + words[i+k]
		}
	}
	return false
}
