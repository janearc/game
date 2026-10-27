package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
)

// noFetch is the fetcher for a build that must not fetch.
func noFetch(url string) ([]byte, error) {
	return nil, errors.New("fetched " + url)
}

// tarball is a gzipped tarball of files under top/, as GitHub makes one,
// with any extra headers written after them.
func tarball(t *testing.T, files map[string]string,
	extra ...tar.Header) []byte {
	t.Helper()
	var zipped bytes.Buffer
	zipper := gzip.NewWriter(&zipped)
	packed := tarWriting{t: t, to: tar.NewWriter(zipper)}
	packed.write(tar.Header{Typeflag: tar.TypeXGlobalHeader,
		Name:       "pax_global_header",
		PAXRecords: map[string]string{"comment": "a commit"}}, "")
	dirMode := int64(os.FileMode(0o755))
	packed.write(tar.Header{Typeflag: tar.TypeDir, Name: "top/",
		Mode: dirMode}, "")
	names := make([]string, count.Nothing, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	fileMode := int64(os.FileMode(0o644))
	for _, name := range names {
		size := int64(len(files[name]))
		packed.write(tar.Header{Typeflag: tar.TypeReg, Name: "top/" + name,
			Mode: fileMode, Size: size}, files[name])
	}
	for _, head := range extra {
		packed.write(head, "")
	}
	if err := errors.Join(packed.to.Close(), zipper.Close()); err != nil {
		t.Fatal(err)
	}
	return zipped.Bytes()
}

// tarWriting is a tarball being written, and the test it is for.
type tarWriting struct {
	t  *testing.T
	to *tar.Writer
}

// write writes one entry, its header and its body.
func (packed tarWriting) write(head tar.Header, body string) {
	if err := packed.to.WriteHeader(&head); err != nil {
		packed.t.Fatal(err)
	}
	if _, err := packed.to.Write([]byte(body)); err != nil {
		packed.t.Fatal(err)
	}
}

// pinned is a dep for body, pinned to its sum.
func pinned(name string, body []byte) dep {
	sum := sha256.Sum256(body)
	commit := strings.Repeat("c", len(fakeObject))
	return dep{name: name, commit: commit,
		url: "https://example.test/" + name, sum: hex.EncodeToString(sum[:])}
}

// serving is a fetcher that answers one body and counts its calls.
type serving struct {
	body  []byte
	err   error
	calls int
}

// get is serving's fetcher.
func (server *serving) get(string) ([]byte, error) {
	server.calls++
	return server.body, server.err
}

// A fetched library is unpacked into bin/.deps/NAME-COMMIT without the
// tarball's top folder, and fetched only once.
func TestFetchDepUnpacksItOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := tarball(t, map[string]string{"inc/lib.h": "int f(void);\n",
		"src/a.c": "int f(void) { return 1; }\n"})
	lib := pinned("lib", body)
	server := &serving{body: body}
	place, err := fetchDep(dir, lib, server.get)
	if err != nil || place != filepath.Join("bin", ".deps", "lib-ccccccc") {
		t.Fatalf("place %q, err %v", place, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, place, "src", "a.c"))
	if err != nil || string(got) != "int f(void) { return 1; }\n" {
		t.Errorf("src/a.c: %q, %v", got, err)
	}
	if _, err := fetchDep(dir, lib, server.get); err != nil ||
		server.calls != count.One {
		t.Errorf("again: err %v, fetched %d times", err, server.calls)
	}
}

// anti-tests: a body that is not the one pinned, and a fetch that fails,
// each leave nothing behind.
func TestFetchDepRefusesWhatWasNotPinned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lib := pinned("lib", []byte("the tarball that was pinned"))
	if _, err := fetchDep(dir, lib, (&serving{body: []byte("another")}).get); !errors.Is(err, errBadSum) {
		t.Errorf("another body: %v", err)
	}
	down := errors.New("no route to host")
	if _, err := fetchDep(dir, lib, (&serving{err: down}).get); !errors.Is(err, down) {
		t.Errorf("a failed fetch: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "bin", ".deps")); count.Positive(len(entries)) {
		t.Errorf("left behind: %v", entries)
	}
}

// anti-tests: an entry stepping out of its folder, an absolute one, a
// link, and a body that is not gzip are each refused, and nothing of the
// library is left.
func TestFetchDepRefusesWhatWouldLandOutside(t *testing.T) {
	t.Parallel()
	for name, extra := range map[string]tar.Header{
		"up":   {Typeflag: tar.TypeReg, Name: "../evil"},
		"abs":  {Typeflag: tar.TypeReg, Name: "/etc/evil"},
		"link": {Typeflag: tar.TypeSymlink, Name: "top/l", Linkname: "/"},
	} {
		dir := t.TempDir()
		body := tarball(t, map[string]string{"a.c": ""}, extra)
		if _, err := fetchDep(dir, pinned("lib", body),
			(&serving{body: body}).get); !errors.Is(err, errUnsafeEntry) {
			t.Errorf("%s: %v", name, err)
		}
		if entries, _ := os.ReadDir(filepath.Join(dir, "bin", ".deps")); count.Positive(len(entries)) {
			t.Errorf("%s left behind: %v", name, entries)
		}
	}
	plain := []byte("not gzip")
	if _, err := fetchDep(t.TempDir(), pinned("lib", plain),
		(&serving{body: plain}).get); err == nil {
		t.Error("a body that is not gzip was unpacked")
	}
}

// gzipped is the bytes gzipped.
func gzipped(t *testing.T, raw []byte) []byte {
	t.Helper()
	var zipped bytes.Buffer
	zipper := gzip.NewWriter(&zipped)
	if _, err := zipper.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return zipped.Bytes()
}

// anti-tests: a tarball cut off part way, a file under what is already a
// file, and a file where a folder already is are each refused.
func TestUnpackRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()
	part := "only part of it"
	var raw bytes.Buffer
	cut := tar.NewWriter(&raw)
	// the header claims twice what follows it
	claimed := int64(len(part) + len(part))
	if err := cut.WriteHeader(&tar.Header{Typeflag: tar.TypeReg,
		Name: "top/a.c", Size: claimed}); err != nil {
		t.Fatal(err)
	}
	_, _ = cut.Write([]byte(part))
	truncated := gzipped(t, raw.Bytes())
	if err := unpack(truncated, t.TempDir()); err == nil {
		t.Error("a tarball cut off part way was unpacked")
	}
	// a tar block is 512 bytes, as tar has defined it: one of noise is
	// read as a header and is none
	const tarBlock = 512
	noise := bytes.Repeat([]byte("x"), tarBlock)
	if err := unpack(gzipped(t, noise), t.TempDir()); err == nil {
		t.Error("gzip of no tarball was unpacked")
	}
	under := tarball(t, map[string]string{"a": "a file", "a/b.c": ""})
	if err := unpack(under, t.TempDir()); err == nil {
		t.Error("a file under a file was written")
	}
	over := tarball(t, nil,
		tar.Header{Typeflag: tar.TypeDir, Name: "top/x/"},
		tar.Header{Typeflag: tar.TypeReg, Name: "top/x"})
	if err := unpack(over, t.TempDir()); err == nil {
		t.Error("a file over a folder was written")
	}
}

// anti-test: an earlier fetch's leftovers that cannot be cleared stop the
// fetch before it unpacks anything.
func TestFetchDepStopsAtLeftoversItCannotClear(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := tarball(t, map[string]string{"a.c": ""})
	lib := pinned("lib", body)
	stuck := filepath.Join(dir, depPlace(lib)+".fetching", "stuck")
	write(t, filepath.Join(stuck, "f"), "")
	if err := os.Chmod(stuck, os.FileMode(0o500)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shutDir{dir: stuck, open: os.FileMode(0o755)}.reopen)
	if _, err := fetchDep(dir, lib, (&serving{body: body}).get); err == nil {
		t.Error("leftovers that could not be cleared were ignored")
	}
}

// Whatever files a tarball holds, unpacking gives them back, byte for
// byte, under the folder it was told.
func TestUnpackGivesBackEveryFile(t *testing.T) {
	t.Parallel()
	prop := unpackProperty{test: t, most: countHeld(t, "drawn-most-files")}
	rapid.Check(t, prop.check)
}

// unpackProperty is the property that whatever files a tarball holds,
// unpacking gives them back, byte for byte, under the folder it was told.
type unpackProperty struct {
	test *testing.T
	most int
}

// check is the property as rapid runs it.
func (prop unpackProperty) check(t *rapid.T) {
	names := rapid.SliceOfNDistinct(
		rapid.StringMatching(`[a-z]{1,6}(/[a-z]{1,6})?\.c`), count.One,
		prop.most, rapid.ID[string]).Draw(t, "names")
	files := map[string]string{}
	for _, name := range names {
		files[name] = rapid.String().Draw(t, name)
	}
	into := filepath.Join(prop.test.TempDir(), "lib")
	if err := unpack(tarball(prop.test, files), into); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		got, err := os.ReadFile(filepath.Join(into, name))
		if err != nil || string(got) != body {
			t.Fatalf("%s: %q, %v; want %q", name, got, err, body)
		}
	}
}

// withinTop takes off the top folder, and refuses what would leave it.
func TestWithinTopKeepsEntriesInside(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{"top/a/b.c": "a/b.c",
		"top": ".", "top/": "."} {
		if got, ok := withinTop(name); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", name, got, ok, want)
		}
	}
	for _, name := range []string{"/etc/x", "..", "../x", "top/../../x"} {
		if got, ok := withinTop(name); ok {
			t.Errorf("%q let in as %q", name, got)
		}
	}
}

// servesLib answers the tarball at /lib and nothing anywhere else.
func servesLib(answer http.ResponseWriter, asked *http.Request) {
	if asked.URL.Path != "/lib" {
		http.NotFound(answer, asked)
		return
	}
	_, _ = answer.Write([]byte("the tarball"))
}

// game's fetcher answers a 200's body, and refuses anything else.
func TestHTTPFetchTakesOnlyA200(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(servesLib))
	defer server.Close()
	if body, err := httpFetch(server.URL + "/lib"); err != nil ||
		string(body) != "the tarball" {
		t.Errorf("200: %q, %v", body, err)
	}
	if _, err := httpFetch(server.URL + "/gone"); err == nil ||
		!strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
	if _, err := httpFetch("http://127.0.0.1:1/lib"); err == nil {
		t.Error("a closed port answered")
	}
}

// Every library game fetches is pinned: a full commit, a sha256, and a
// url naming that commit.
func TestEveryDepIsPinned(t *testing.T) {
	t.Parallel()
	commit := regexp.MustCompile(`^[0-9a-f]{40}$`)
	sum := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for name, each := range deps {
		if each.name != name || !commit.MatchString(each.commit) ||
			!sum.MatchString(each.sum) ||
			!strings.HasPrefix(each.url, "https://codeload.github.com/") ||
			!strings.HasSuffix(each.url, "/tar.gz/"+each.commit) {
			t.Errorf("%s: %+v", name, each)
		}
	}
}

// -with names are taken in order; a name game does not fetch is refused.
func TestDepsForTakesOnlyWhatGameFetches(t *testing.T) {
	t.Parallel()
	asked := []string{"swiftqc", "theft"}
	got, err := depsFor(asked)
	var names []string
	for _, each := range got {
		names = append(names, each.name)
	}
	if err != nil || !slices.Equal(names, asked) {
		t.Errorf("got %+v, %v", got, err)
	}
	if _, err := depsFor([]string{"theft", "nosuch"}); !errors.Is(err, errNoDep) {
		t.Errorf("nosuch: %v", err)
	}
}

// depAt is a library fetched into its place in a tree.
type depAt struct{ dir, place string }

// headers is where its headers are.
func (at depAt) headers() string { return filepath.Join(at.place, "inc") }

// object is where the source's object goes.
func (at depAt) object(name string) string {
	return filepath.Join(at.place, "obj", name+".o")
}

// compile is the call that builds the source.
func (at depAt) compile(name string) call {
	return call{at.dir, "cc", []string{"-std=c99", "-O2", "-DX", "-I",
		at.headers(), "-c", filepath.Join(at.place, "src", name+".c"), "-o",
		at.object(name)}}
}

// A C library's sources are each built with its own flags into obj/ in
// its place, and the program gets its headers, objects and libraries.
func TestCDepsBuildsEachSourceAndLinksIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := tarball(t, map[string]string{"inc/lib.h": "", "src/a.c": "",
		"src/b.c": ""})
	lib := pinned("lib", body)
	lib.include, lib.sources = "inc", "src/*.c"
	lib.defines, lib.libs = []string{"-DX"}, []string{"-lm"}
	compiler := &tools{}
	add, err := cDeps(dir, []dep{lib}, (&serving{body: body}).get,
		compiler.run)
	place := depPlace(lib)
	at := depAt{dir: dir, place: place}
	if err != nil || !slices.EqualFunc(compiler.calls,
		[]call{at.compile("a"), at.compile("b")}, sameCall) {
		t.Errorf("err %v, calls %+v", err, compiler.calls)
	}
	want := linked{includes: []string{"-I", at.headers()},
		objects: []string{at.object("a"), at.object("b")},
		libs:    []string{"-lm"}}
	if !slices.Equal(add.includes, want.includes) ||
		!slices.Equal(add.objects, want.objects) ||
		!slices.Equal(add.libs, want.libs) {
		t.Errorf("linked %+v, want %+v", add, want)
	}
}

// anti-tests: a library that is not C is refused before any fetch; one
// with no sources, one whose fetch fails, and one whose source does not
// compile are each refused.
func TestCDepsRefusesWhatItCannotLink(t *testing.T) {
	t.Parallel()
	server := &serving{}
	compiler := &tools{}
	if _, err := cDeps(t.TempDir(), []dep{deps["fox"]}, server.get,
		compiler.run); !errors.Is(err, errNotCLib) ||
		count.Positive(server.calls) {
		t.Errorf("fox: %v, fetched %d times", err, server.calls)
	}
	headersOnly := tarball(t, map[string]string{"inc/lib.h": ""})
	empty := pinned("lib", headersOnly)
	empty.include, empty.sources = "inc", "src/*.c"
	if _, err := cDeps(t.TempDir(), []dep{empty},
		(&serving{body: headersOnly}).get, compiler.run); !errors.Is(err,
		errNoCSources) {
		t.Errorf("no sources: %v", err)
	}
	down := errors.New("no route to host")
	if _, err := cDeps(t.TempDir(), []dep{empty}, (&serving{err: down}).get,
		compiler.run); !errors.Is(err, down) {
		t.Errorf("a failed fetch: %v", err)
	}
	body := tarball(t, map[string]string{"inc/lib.h": "", "src/a.c": ""})
	broken := pinned("lib", body)
	broken.include, broken.sources = "inc", "src/*.c"
	bad := errors.New("cc: a.c:1: error")
	failing := &tools{fail: map[string]error{"cc": bad}}
	if _, err := cDeps(t.TempDir(), []dep{broken}, (&serving{body: body}).get,
		failing.run); !errors.Is(err, bad) {
		t.Errorf("a source that does not compile: %v", err)
	}
}

// a file where a library's obj/ goes is refused.
func TestCDepsRefusesAFileWhereObjGoes(t *testing.T) {
	t.Parallel()
	body := tarball(t, map[string]string{"inc/lib.h": "", "src/a.c": ""})
	broken := pinned("lib", body)
	broken.include, broken.sources = "inc", "src/*.c"
	full := t.TempDir()
	place := filepath.Join(full, depPlace(broken))
	write(t, filepath.Join(place, "obj"), "")
	write(t, filepath.Join(place, "src", "a.c"), "")
	if _, err := cDeps(full, []dep{broken}, noFetch,
		(&tools{}).run); err == nil {
		t.Error("a file where obj/ goes was taken")
	}
}

// A library's words go in their places: headers after the flags, objects
// and libraries after the sources, -o last.
func TestWithLinkedPutsEachWordInItsPlace(t *testing.T) {
	t.Parallel()
	argv, _, _ := cArgv("", []string{"a.c", "b.c"})
	got := withLinked(argv, linked{includes: []string{"-I", "inc"},
		objects: []string{"x.o"}, libs: []string{"-lm"}})
	want := slices.Concat(cFlags, []string{"-I", "inc", "a.c", "b.c",
		"x.o", "-lm", "-o", "bin/a"})
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// byURL is a fetcher that answers each url its own body.
type byURL map[string][]byte

// get is byURL's fetcher.
func (bodies byURL) get(url string) ([]byte, error) {
	body, ok := bodies[url]
	if !ok {
		return nil, errors.New("no such url " + url)
	}
	return body, nil
}

// fetchAll fetches each library and says where each one is, in order.
func TestFetchAllSaysWhereEachIs(t *testing.T) {
	t.Parallel()
	one, two := tarball(t, map[string]string{"a.c": "1"}),
		tarball(t, map[string]string{"b.c": "2"})
	first, second := pinned("one", one), pinned("two", two)
	var out bytes.Buffer
	err := fetchAll(t.TempDir(), []dep{first, second},
		byURL{first.url: one, second.url: two}.get, &out)
	want := "one is in " + depPlace(first) + "\ntwo is in " +
		depPlace(second) + "\n"
	if err != nil || out.String() != want {
		t.Errorf("err %v, said %q", err, out.String())
	}
}

// anti-test: fetchAll stops at the first library it cannot fetch.
func TestFetchAllStopsAtAFailedFetch(t *testing.T) {
	t.Parallel()
	one := tarball(t, map[string]string{"a.c": "1"})
	first, missing := pinned("one", one), pinned("gone", []byte("x"))
	var out bytes.Buffer
	if err := fetchAll(t.TempDir(), []dep{missing, first},
		byURL{first.url: one}.get, &out); err == nil ||
		count.Positive(out.Len()) {
		t.Errorf("err %v, said %q", err, out.String())
	}
}

// anti-tests: build-xcode refuses a library it does not fetch before
// anything runs, and a failed fetch stops it before xcodebuild.
func TestBuildXcodeRefusesALibraryItCannotFetch(t *testing.T) {
	t.Parallel()
	builder := &tools{}
	var out, errs bytes.Buffer
	if err := buildXcodeBy(t.TempDir(), []string{"-scheme", "a", "-with",
		"nosuch"}, builder.run, noFetch, &out, &errs); !errors.Is(err,
		errNoDep) {
		t.Errorf("nosuch: %v", err)
	}
	if err := buildXcodeBy(t.TempDir(), []string{"-scheme", "a", "-with",
		"fox"}, builder.run, noFetch, &out, &errs); err == nil ||
		!strings.Contains(err.Error(), "fetching fox") {
		t.Errorf("a failed fetch: %v", err)
	}
	if count.Positive(len(builder.calls)) {
		t.Errorf("xcodebuild asked: %+v", builder.calls)
	}
}

// End to end: theft fetched from GitHub at its pinned commit, built and
// linked, and a real property run. It needs the network, so -short and a
// machine that cannot reach GitHub leave it out.
func TestBuildCWithTheftRunsARealProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: no network")
	}
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no cc on this machine")
	}
	if _, err := httpFetch("https://codeload.github.com/"); err != nil &&
		!strings.Contains(err.Error(), "answered") {
		t.Skip("GitHub cannot be reached: " + err.Error())
	}
	dir := t.TempDir()
	prop := "#include <stdint.h>\n#include <stdio.h>\n#include \"theft.h\"\n" +
		"static enum theft_trial_res prop(struct theft *t, void *arg) {\n" +
		"  (void)t;\n  uint32_t x = *(uint32_t *)arg;\n" +
		"  return (x ^ x) == 0 ? THEFT_TRIAL_PASS : THEFT_TRIAL_FAIL;\n}\n" +
		"int main(void) {\n  struct theft_run_config config = {\n" +
		"    .name = \"self xor is zero\", .prop1 = prop,\n" +
		"    .type_info = {theft_get_builtin_type_info(" +
		"THEFT_BUILTIN_uint32_t)},\n" +
		"    .seed = theft_seed_of_time(), .trials = 200,\n  };\n" +
		"  return theft_run(&config) == THEFT_RUN_PASS ? 0 : 1;\n}\n"
	write(t, filepath.Join(dir, "prop.c"), prop)
	if err := buildCVerb(repoAt(dir), blankConfig(),
		[]string{"-with", "theft", "prop.c"}); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(filepath.Join(dir, "bin", "prop")).Run(); err != nil {
		t.Errorf("bin/prop: %v", err)
	}
}

// anti-tests: build-c refuses a library it does not fetch, or cannot
// link, before cc runs.
func TestBuildCRefusesALibraryItCannotLink(t *testing.T) {
	t.Parallel()
	compiler := &tools{}
	var out, errs bytes.Buffer
	if err := buildCBy(t.TempDir(), []string{"-with", "nosuch", "a.c"},
		compiler.run, noFetch, &out, &errs); !errors.Is(err, errNoDep) {
		t.Errorf("nosuch: %v", err)
	}
	if err := buildCBy(t.TempDir(), []string{"-with", "fox", "a.c"},
		compiler.run, noFetch, &out, &errs); !errors.Is(err, errNotCLib) {
		t.Errorf("fox: %v", err)
	}
	if count.Positive(len(compiler.calls)) {
		t.Errorf("cc asked: %+v", compiler.calls)
	}
}
