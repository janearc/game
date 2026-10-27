package sweep

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode"

	"github.com/janearc/game/internal/count"
)

// baked is the hashes file cmd/never writes. git ignores it, so a game
// built from a clone has no never-words and refuses to release.
//
//go:embed never
var baked embed.FS

// Never holds a salt and the hashes of the never-words. it holds no word.
type Never struct {
	salt   string
	hashes map[string]bool
}

// neverWord is the kind of hit a never-word makes. it cannot say which one.
const neverWord = "a never-word"

// longest is the most words a never-word may run to: three, by
// definition, a word or a name of two or three.
const longest = 3

// Embedded is the files this build was baked with: never/hashes among them
// when cmd/never wrote one, none in a build from a clone.
func Embedded() fs.FS { return baked }

// Baked reads never/hashes from files, or returns empty if missing.
func Baked(files fs.FS) Never {
	body, err := fs.ReadFile(files, "never/hashes")
	if err != nil {
		return Never{}
	}
	return parseNever(string(body))
}

// Empty is true when there are no never-words.
func (never Never) Empty() bool {
	return count.None(len(never.hashes))
}

// Bake is the hashes file for a list of never-words. salt is first,
// then sorted hashes. each entry is one to three words, case and
// punctuation ignored. any other entry is an error with its place.
func Bake(salt string, list []string) (string, error) {
	var hashes []string
	for place, entry := range list {
		run := wordsOf(entry)
		if count.None(len(run)) || len(run) > longest {
			return "", fmt.Errorf(
				"never-word %d is %d words; a never-word "+
					"is a word or a name of two or three",
				count.Next(place), len(run),
			)
		}
		joined := strings.Join(run, " ")
		hashes = append(hashes, hashOf(salt, joined))
	}
	sort.Strings(hashes)
	return "salt " + salt + "\n" + strings.Join(hashes, "\n") + "\n", nil
}

// parseNever reads a hashes file written by cmd/never.
func parseNever(body string) Never {
	never := Never{hashes: map[string]bool{}}
	first, rest, _ := strings.Cut(body, "\n")
	never.salt = strings.TrimPrefix(strings.TrimSpace(first), "salt ")
	for _, line := range strings.Split(rest, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			never.hashes[line] = true
		}
	}
	return never
}

// hashOf is the hex sha-256 of salt, a nul, then run. run must be lower
// case with one space between words.
func hashOf(salt, run string) string {
	sum := sha256.Sum256([]byte(salt + "\x00" + run))
	return hex.EncodeToString(sum[:])
}

// wordsOf splits text into lower case words. a word is letters and digits.
func wordsOf(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), betweenWords)
}

// betweenWords is whether a character is not part of a word.
func betweenWords(char rune) bool {
	return !unicode.IsLetter(char) && !unicode.IsDigit(char)
}

// found checks if text contains a never-word.
func (never Never) found(text string) bool {
	if never.Empty() {
		return false
	}
	words := wordsOf(text)
	for i := range words {
		run := words[i]
		for k := count.One; ; k++ {
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
