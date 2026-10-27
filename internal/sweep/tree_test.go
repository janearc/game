package sweep_test

import (
	"fmt"
	"github.com/janearc/game/internal/gittest"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// the fixtures are assembled at run time, like the patterns' own tests,
// so that this file spells no machine path, no address and no uuid:
// the sweep reads this tree too.
var (
	machinePath = "kept at /Us" + "ers/ada/work"
	theAuthor   = "Ada Lovelace"
)

// treeMem is a repository in memory with one commit of the files given.
// It lives as long as the test.
func treeMem(t *testing.T, files map[string]string) *gittest.Mem {
	t.Helper()
	mem, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Commit("fixture", files); err != nil {
		t.Fatal(err)
	}
	return mem
}

// treeFixture is a repository with one commit of the files given.
func treeFixture(t *testing.T, files map[string]string) repo.Repo {
	t.Helper()
	return repo.At("/work", treeMem(t, files))
}

// named is whether one of the hits reads as the given "kind in file".
func named(hits []sweep.Hit, want string) bool {
	lines := strings.Split(kinds(hits), "\n")
	return slices.Contains(lines, want)
}

// kinds is the hits as "kind in file", sorted, for comparing.
func kinds(hits []sweep.Hit) string {
	var out []string
	for _, hit := range hits {
		out = append(out, hit.String())
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// a tree with one of everything. the sweep names each leak once, in
// its file; lets a licence name the author; lets a file carry what
// .game allows it and no more; skips what is excluded; reads a file with
// a nul in it as it reads any other; reads a text file past its first
// kilobyte; and finds nothing in what is clean.
func TestTreeNamesEachLeakInItsFile(t *testing.T) {
	never := bakedFrom(t, "a-salt", []string{"zorblax"})
	kilobyte := int(held(t, "kilobyte"))
	tree := treeFixture(t, map[string]string{
		"notes.md":         "the file was " + machinePath + "\n",
		"README.md":        "written by ada lovelace, with care\n",
		"LICENSE":          "copyright " + theAuthor + "\n",
		"docs/LICENCE.txt": theAuthor + " holds the copyright\n",
		"secret.txt":       "the word is Zorblax, said twice\n",
		"bin.dat":          "\x00\x01\x02 " + machinePath,
		"big.txt": strings.Repeat("x", kilobyte) + "\x00" +
			machinePath,
		"vendor/x.txt": machinePath,
		"allowed.md":   machinePath + "\n",
		"byline.md":    "by " + theAuthor + "\n",
		"clean.go":     "package clean\n",
	})
	allow := map[string][]string{
		"allowed.md": {"paths"},
		"byline.md":  {"author"},
	}
	hits, err := sweep.Tree(tree, "HEAD", never, theAuthor, allow,
		"vendor/")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		neverKind + " in secret.txt",
		"machine paths in big.txt",
		"machine paths in bin.dat",
		"machine paths in notes.md",
		"the author " + theAuthor + " in README.md",
	}, "\n")
	if got := kinds(hits); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// with no author given, nothing is checked for one: a tree that names
// a person is not a tree that names the author.
func TestTreeWithoutAnAuthorLooksForNone(t *testing.T) {
	tree := treeFixture(t, map[string]string{
		"README.md": "by " + theAuthor + "\n",
	})
	hits, err := sweep.Tree(tree, "HEAD", sweep.Never{}, "", nil)
	if err != nil || count.Positive(len(hits)) {
		t.Errorf("hits %v, err %v", hits, err)
	}
}

// a tree entry git lists but cannot show, a submodule's commit say, is
// an error and not a file quietly skipped.
func TestTreeStopsOnAnEntryItCannotShow(t *testing.T) {
	mem := treeMem(t, map[string]string{"a.txt": "fine\n"})
	// a git object name is forty hex digits, as sha-1 has defined them.
	const fakeObject = "0123456789abcdef0123456789abcdef01234567"
	if _, err := mem.Gitlink("sub", fakeObject); err != nil {
		t.Fatal(err)
	}
	tree := repo.At("/work", mem)
	_, err := sweep.Tree(tree, "HEAD", sweep.Never{}, "", nil)
	if err == nil || !strings.Contains(err.Error(), "sub") {
		t.Errorf("want an error naming sub, got %v", err)
	}
}

// neverKind is the kind of hit a never-word makes. it cannot say which
// one.
const neverKind = "a never-word"

// proseKinds sweeps each text as a file of its own, in one tree, and
// says for each the kinds it was hit for, in order, joined by commas.
func proseKinds(
	t *testing.T, never sweep.Never, texts ...string,
) []string {
	t.Helper()
	files := map[string]string{}
	for place, text := range texts {
		files[fmt.Sprintf("t%d.txt", place)] = text
	}
	hits, err := sweep.Tree(treeFixture(t, files), "HEAD", never, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	said := make([]string, len(texts))
	for _, hit := range hits {
		whole := hit.String()
		at := strings.LastIndex(whole, " in t")
		number := strings.TrimSuffix(whole[at+len(" in t"):], ".txt")
		place, err := strconv.Atoi(number)
		if err != nil {
			t.Fatalf("a hit in no file of ours: %q", whole)
		}
		said[place] += whole[:at] + ","
	}
	return said
}

// filesHit is the files among the given that the sweep names the author
// in, sorted. a licence may name the author; no other file may.
func filesHit(t *testing.T, names []string) []string {
	t.Helper()
	files := map[string]string{}
	for _, name := range names {
		files[name] = "by " + theAuthor + "\n"
	}
	hits, err := sweep.Tree(treeFixture(t, files), "HEAD", sweep.Never{},
		theAuthor, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hit []string
	for _, one := range hits {
		_, file, _ := strings.Cut(one.String(), theAuthor+" in ")
		hit = append(hit, file)
	}
	sort.Strings(hit)
	return hit
}

// a licence is known by its base name, in any case, with any extension,
// at any depth; and nothing else is.
func TestIsLicenceByBaseName(t *testing.T) {
	licences := []string{
		"LICENSE", "LICENCE", "COPYING", "docs/license",
		"docs/LICENSE.txt", "COPYING.md", "a/b/Licence.rst",
	}
	if hit := filesHit(t, licences); len(hit) != count.Nothing {
		t.Errorf("a licence was swept for its author: %v", hit)
	}
	others := []string{
		"LICENSES.md", "README.md", "x.LICENSE", "LICENSE/notes.txt",
		".license",
	}
	want := slices.Clone(others)
	sort.Strings(want)
	if hit := filesHit(t, others); !slices.Equal(hit, want) {
		t.Errorf("swept for the author: %v, want every one of %v", hit, want)
	}
}

// Every file is read whatever its bytes. Nuls early or late do not make
// the sweep pass over the file.
func TestAFileWithANulIsStillSwept(t *testing.T) {
	kilobyte := int(held(t, "kilobyte"))
	kb := strings.Repeat("x", kilobyte)
	cases := []struct {
		name, body string
		swept      bool
	}{
		{"empty", "", true},
		{"plain", "plain text\n", true},
		{"early nul", "a\x00b", true},
		{"a kilobyte of text", kb, true},
		{"nul just past the kilobyte", kb + "\x00", true},
		{"nul at the kilobyte's last byte",
			kb[:count.Last(kilobyte)] + "\x00", true},
	}
	files := map[string]string{}
	for place, each := range cases {
		files[fmt.Sprintf("f%d", place)] = each.body + machinePath
	}
	hits, err := sweep.Tree(treeFixture(t, files), "HEAD", sweep.Never{},
		"", nil)
	if err != nil {
		t.Fatal(err)
	}
	for place, each := range cases {
		name := fmt.Sprintf("machine paths in f%d", place)
		got := named(hits, name)
		if got != each.swept {
			t.Errorf("%s: swept %v, want %v", each.name, got, each.swept)
		}
	}
}

// one piece of prose with one leak of each kind names that kind once,
// and a plain subject names nothing. a trailer is a history's, not a
// file's, and has its own test.
func TestTextNamesEachKindOnce(t *testing.T) {
	never := bakedFrom(t, "a-salt", []string{"zorblax"})
	at := "@"
	uuid := strings.Join(
		[]string{"0123abcd", "0123", "abcd", "0123", "abcd0123abcd"},
		"-",
	)
	cases := map[string]string{
		"session urls":    "see https://cla" + "ude.ai/code/x",
		"uuids":           "seen in " + uuid + " yesterday",
		"machine paths":   machinePath,
		"email addresses": "write to ada" + at + "ada.dev",
		neverKind:         "and ZORBLAX again",
	}
	files := map[string]string{"plain.txt": "a plain subject"}
	var want []string
	for kind, text := range cases {
		file := strings.ReplaceAll(kind, " ", "-") + ".txt"
		files[file] = text
		want = append(want, kind+" in "+file)
	}
	sort.Strings(want)
	hits, err := sweep.Tree(treeFixture(t, files), "HEAD", never, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(hits); got != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
}

// scratch is a repository that is staged and swept again for each text a
// property draws, without a commit for each.
type scratch struct{ mem *gittest.Mem }

// newScratch is a repository holding one clean file.
func newScratch(t *testing.T) scratch {
	t.Helper()
	return scratch{mem: treeMem(t, map[string]string{"clean": "x\n"})}
}

// hits is, for each text, whether sweeping it as a file of the
// repository is hit for a never-word. the texts are swept in one tree.
func (scratched scratch) hits(
	test failer, never sweep.Never, texts ...string,
) []bool {
	test.Helper()
	var names []string
	for place, text := range texts {
		name := fmt.Sprintf("prose%d.txt", place)
		names = append(names, name)
		if err := scratched.mem.Write(name, text); err != nil {
			test.Fatal(err)
		}
		if err := scratched.mem.Stage(name); err != nil {
			test.Fatal(err)
		}
	}
	tree, err := scratched.mem.Staged()
	if err != nil {
		test.Fatal(err)
	}
	work := repo.At("/work", scratched.mem)
	found, err := sweep.Tree(work, tree, never, "", nil)
	if err != nil {
		test.Fatal(err)
	}
	said := make([]bool, len(texts))
	for place, name := range names {
		said[place] = named(found, neverKind+" in "+name)
	}
	return said
}

// failer is what a test and a property both are: something that can
// stop on an error.
type failer interface {
	Helper()
	Fatal(...any)
}
