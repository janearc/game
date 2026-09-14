package sweep

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// the word list reads one word per line and ignores blanks and comments.
func TestWords(t *testing.T) {
	p := filepath.Join(t.TempDir(), "words")
	os.WriteFile(p, []byte("# names\nzebra\n\nquokka\n"), 0o644)
	w := Words(p)
	if len(w) != 2 || w[0] != "zebra" {
		t.Fatalf("got %v", w)
	}
	if len(Words(p+".missing")) != 0 {
		t.Error("a missing list was not empty")
	}
}

// the patterns find what they are for. the fixtures are assembled at
// run time so this file spells none of them, and the last check proves
// that by sweeping this file's own source with every pattern.
func TestPatterns(t *testing.T) {
	uuid := strings.Join([]string{"0123abcd", "0123", "abcd", "0123", "abcd0123abcd"}, "-")
	cases := map[string]string{
		"urls":   "see https://cla" + "ude.ai/code/sess" + "ion_0123abcd",
		"uuids":  "id " + uuid,
		"paths":  "kept at /Us" + "ers/someone/x",
		"emails": "write to a.b" + "@" + "c.de",
	}
	if !urls.MatchString(cases["urls"]) || !uuids.MatchString(cases["uuids"]) || !paths.MatchString(cases["paths"]) || !emails.MatchString(cases["emails"]) {
		t.Fatal("a pattern missed its case")
	}
	if !trailers.MatchString("Co-Auth" + "ored-By: x") {
		t.Fatal("the trailer pattern missed its case")
	}
	for _, f := range []string{"sweep.go", "sweep_test.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for name, re := range map[string]interface{ MatchString(string) bool }{"urls": urls, "uuids": uuids, "paths": paths, "emails": emails, "trailers": trailers} {
			if re.MatchString(string(src)) {
				t.Errorf("%s matches %s", name, f)
			}
		}
	}
}
