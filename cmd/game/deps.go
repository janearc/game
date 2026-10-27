package main

// Libraries game fetches for a build that asks for one with -with NAME.
// Each is a source tarball at a pinned commit, held to its sha256, and
// unpacked into bin/.deps/ under its name and commit, so game clean
// takes it with the rest of bin/.
//
// A tarball that does not match its sum is refused.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// dep is a library game can fetch. A C library also says where its
// headers and sources are, and how they are built and linked.
type dep struct {
	name, commit, url, sum string
	include, sources       string
	defines, libs          []string
}

// deps are the libraries game can fetch, by the name -with takes.
var deps = map[string]dep{
	"theft": {
		name:   "theft",
		commit: "62e093d9e33bb4218736dce2535eedda2904b8ba",
		url: "https://codeload.github.com/silentbicycle/theft/tar.gz/" +
			"62e093d9e33bb4218736dce2535eedda2904b8ba",
		sum: "681739355ad7c2d63d3f58b22b9a9cfe1e8ce2d2" +
			"9c6bcb803a8c4490be90b4ac",
		include: "inc", sources: "src/*.c",
		defines: []string{"-D_POSIX_C_SOURCE=199309L", "-D_C99_SOURCE"},
		libs:    []string{"-lm"},
	},
	"fox": {
		name:   "fox",
		commit: "766bceb7c28b516d45e93c744a309bf0a5757c2d",
		url: "https://codeload.github.com/jeffh/Fox/tar.gz/" +
			"766bceb7c28b516d45e93c744a309bf0a5757c2d",
		sum: "06f58623b956a0e6ab0dda19025ca4dd8e793982" +
			"027225a1b259e2525721b733",
	},
	"swiftqc": {
		name:   "swiftqc",
		commit: "f9f3fb1f33b54742e9863745db9ccd8dd47e1076",
		url: "https://codeload.github.com/Aristide021/SwiftQC/tar.gz/" +
			"f9f3fb1f33b54742e9863745db9ccd8dd47e1076",
		sum: "dcbebfbd9276aadd26eff98534378a3280499e2e" +
			"a4d53d3140228b21b2ef464d",
	},
}

// errNoDep is -with naming a library game does not know.
var errNoDep = errors.New("is no library game fetches")

// errBadSum is a download that is not the tarball pinned.
var errBadSum = errors.New("does not match its pinned sha256")

// errUnsafeEntry is a tarball entry that would land outside its folder,
// or is not a plain file or folder.
var errUnsafeEntry = errors.New("is not a file or folder inside the library")

// fetcher fetches a url's bytes.
type fetcher func(url string) ([]byte, error)

// depPlace is where d is unpacked, relative to the tree.
func depPlace(lib dep) string {
	short := repo.Short(lib.commit)
	return filepath.Join("bin", ".deps", lib.name+"-"+short)
}

// fetchDep makes sure d is unpacked in dir, fetching it with get if it is
// not there yet, and gives its place relative to the tree. Nothing is left
// behind when the fetch, the sum or the unpacking fails.
func fetchDep(dir string, lib dep, get fetcher) (string, error) {
	place := depPlace(lib)
	if _, err := os.Stat(filepath.Join(dir, place)); err == nil {
		return place, nil
	}
	body, err := get(lib.url)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", lib.name, err)
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != lib.sum {
		return "", fmt.Errorf("%s %w", lib.name, errBadSum)
	}
	staging := filepath.Join(dir, place+".fetching")
	if err := os.RemoveAll(staging); err != nil {
		return "", err
	}
	if err := unpack(body, staging); err != nil {
		os.RemoveAll(staging)
		return "", fmt.Errorf("%s: %w", lib.name, err)
	}
	return place, os.Rename(staging, filepath.Join(dir, place))
}

// unpack writes a gzipped tarball's files and folders under into, with
// the tarball's own top folder taken off. It refuses an entry that would
// land outside into, and anything that is not a file or a folder.
func unpack(body []byte, into string) error {
	zipped, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	archive := tar.NewReader(zipped)
	for {
		head, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if head.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		inside, ok := withinTop(head.Name)
		if !ok || (head.Typeflag != tar.TypeDir &&
			head.Typeflag != tar.TypeReg) {
			return fmt.Errorf("%s %w", head.Name, errUnsafeEntry)
		}
		err = place(archive, head, filepath.Join(into, inside))
		if err != nil {
			return err
		}
	}
}

// withinTop is name without the tarball's top folder, and whether what is
// left stays inside: not absolute, and no step up.
func withinTop(name string) (string, bool) {
	clean := path.Clean(name)
	if path.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, "../") {
		return "", false
	}
	_, inside, found := strings.Cut(clean, "/")
	if !found {
		return ".", true
	}
	return inside, true
}

// place writes one entry: a folder, or a file with its bytes.
func place(from io.Reader, head *tar.Header, to string) error {
	if head.Typeflag == tar.TypeDir {
		return os.MkdirAll(to, folderMode)
	}
	if err := os.MkdirAll(filepath.Dir(to), folderMode); err != nil {
		return err
	}
	file, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		ownerWrites)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, from)
	return errors.Join(err, file.Close())
}

// fetchTimeout is how long one download may take: two minutes, defined
// here as enough for a tarball on a slow day.
const fetchTimeout = 2 * time.Minute

// httpFetch is game's fetcher: one GET, refused unless it answers 200.
func httpFetch(url string) ([]byte, error) {
	client := http.Client{Timeout: fetchTimeout}
	answer, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, answer.Status)
	}
	return io.ReadAll(answer.Body)
}

// errNotCLib is -with naming a library build-c cannot link.
var errNotCLib = errors.New("is not a C library build-c can link")

// errNoCSources is a fetched C library with none of its sources.
var errNoCSources = errors.New("has none of the C sources it should")

// linked is what C libraries add to a program's cc words: their headers,
// their built objects, and what they link against.
type linked struct {
	includes, objects, libs []string
}

// cDeps fetches each library with get and builds its sources with by,
// with its own flags, into obj/ in its place; it gives what the program's
// build adds. It refuses a library that is not C.
func cDeps(dir string, libs []dep, get fetcher, by runner) (linked, error) {
	var add linked
	for _, lib := range libs {
		if lib.include == "" {
			return linked{}, fmt.Errorf("-with %s %w", lib.name,
				errNotCLib)
		}
		place, err := fetchDep(dir, lib, get)
		if err != nil {
			return linked{}, err
		}
		headers := filepath.Join(place, lib.include)
		sources, _ := filepath.Glob(
			filepath.Join(dir, place, lib.sources))
		if count.None(len(sources)) {
			return linked{}, fmt.Errorf("%s %w", lib.name,
				errNoCSources)
		}
		objects := filepath.Join(place, "obj")
		err = os.MkdirAll(filepath.Join(dir, objects), folderMode)
		if err != nil {
			return linked{}, err
		}
		for _, source := range sources {
			relative, _ := filepath.Rel(dir, source)
			stem := strings.TrimSuffix(filepath.Base(source), ".c")
			object := filepath.Join(objects, stem+".o")
			words := []string{"-std=c99", "-O2"}
			words = append(words, lib.defines...)
			words = append(words, "-I", headers, "-c", relative,
				"-o", object)
			if _, err := by(dir, cCompiler, words...); err != nil {
				return linked{}, err
			}
			add.objects = append(add.objects, object)
		}
		add.includes = append(add.includes, "-I", headers)
		add.libs = append(add.libs, lib.libs...)
	}
	return add, nil
}

// withLinked puts what libraries add into a program's cc words: the
// headers after the flags, the objects and libraries after the sources,
// before -o.
func withLinked(argv []string, add linked) []string {
	flags, rest := argv[:len(cFlags)], argv[len(cFlags):]
	at := len(rest) - outputWords
	sources, output := rest[:at], rest[at:]
	words := append([]string{}, flags...)
	words = append(words, add.includes...)
	words = append(words, sources...)
	words = append(words, add.objects...)
	words = append(words, add.libs...)
	return append(words, output...)
}

// fetchAll fetches each library into dir with get, and says on out where
// each is, for a build that points at it by path.
func fetchAll(dir string, libs []dep, get fetcher, out io.Writer) error {
	for _, lib := range libs {
		place, err := fetchDep(dir, lib, get)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s is in %s\n", lib.name, place)
	}
	return nil
}

// depsFor is the libraries -with named, in order. It refuses a name game
// does not fetch.
func depsFor(names []string) ([]dep, error) {
	found := make([]dep, count.Nothing, len(names))
	for _, name := range names {
		lib, ok := deps[name]
		if !ok {
			return nil, fmt.Errorf("-with %s %w", name, errNoDep)
		}
		found = append(found, lib)
	}
	return found, nil
}
