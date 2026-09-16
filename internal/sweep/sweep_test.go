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
	uuid := strings.Join(
		[]string{"0123abcd", "0123", "abcd", "0123", "abcd0123abcd"},
		"-",
	)
	cases := map[string]string{
		"urls": "see https://cla" + "ude.ai/code/sess" +
			"ion_0123abcd",
		"uuids":  "id " + uuid,
		"paths":  "kept at /Us" + "ers/someone/x",
		"emails": "write to a.b" + "@" + "c.de",
	}
	if !urls.MatchString(cases["urls"]) ||
		!uuids.MatchString(cases["uuids"]) ||
		!paths.MatchString(cases["paths"]) ||
		!emails.MatchString(cases["emails"]) {
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
		res := map[string]interface{ MatchString(string) bool }{
			"urls":     urls,
			"uuids":    uuids,
			"paths":    paths,
			"trailers": trailers,
		}
		for name, re := range res {
			if re.MatchString(string(src)) {
				t.Errorf("%s matches %s", name, f)
			}
		}
		// an address in a documentation domain is allowed here, the
		// same as anywhere else; anybody's address is not.
		if mailed(string(src)) {
			t.Errorf("a real address is in %s", f)
		}
	}
}

// an address in a documentation domain is nobody's and does not stop a
// release; a real one does.
func TestReservedDomainsAreNotAddresses(t *testing.T) {
	for _, text := range []string{
		"set CONTACT ada@example.com",
		"ada@example.org and bob@example.net",
		"someone@thing.example",
		"contact ada@example.invalid",
		"dist git@github.com:ada/game.git",
	} {
		if mailed(text) {
			t.Errorf("a documentation address was taken for a real one: %q",
				text)
		}
	}
	// built from pieces so that no address appears in this file: the
	// sweep reads its own source and must find none.
	at := "@"
	for _, text := range []string{
		"write to ada" + at + "ada.dev",
		"set CONTACT ada@example.com, bob" + at + "bob.nl",
	} {
		if !mailed(text) {
			t.Errorf("a real address was missed: %q", text)
		}
	}
}

// TestBareUUID is the record id in a catalogue link, which is not a
// session, against the same id on its own, which might be.
func TestBareUUID(t *testing.T) {
	// assembled from pieces, like the patterns, so the sweep of this
	// tree does not find its own fixture
	id := "22be4b55-2466-" + "4320-e053-10a3070a5236"
	if bare("index_url: https://ecat.example.org/records/" + id) {
		t.Error("a uuid inside a link was counted")
	}
	if !bare("seen in " + id + " yesterday") {
		t.Error("a bare uuid was missed")
	}
}
