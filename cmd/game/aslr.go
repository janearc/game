package main

// game aslr: whether the binaries game builds can be loaded anywhere.
// ASLR is a property of a Mach-O executable's header, so the check reads
// it back with otool and refuses an executable without PIE.
//
// A dylib and a framework bundle are position-independent by their
// filetype and pass; only an executable slice is asked for the flag.

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// otool is the program game reads a Mach-O header from.
const otool = "otool"

// errASLRLanguage is aslr with a word that is not c, xcode or go.
var errASLRLanguage = errors.New("aslr: name c, xcode or go")

// errNoBinaries is aslr with no Mach-O binary to check.
var errNoBinaries = errors.New(
	"aslr: no Mach-O binary in bin/; game build first")

// errNoHeader is otool output game cannot read a Mach header from.
var errNoHeader = errors.New("aslr: an otool line it cannot read")

// machoMagics are the first four bytes of a Mach-O file: the thin 32-bit
// and 64-bit forms in either byte order, and the fat container, each
// as mach-o/loader.h and mach-o/fat.h have defined them.
//
//ninja:vow table the magics are defined by mach-o's headers, not measured
var machoMagics = [][]byte{
	{0xfe, 0xed, 0xfa, 0xce}, // MH_MAGIC, defined in mach-o/loader.h
	{0xce, 0xfa, 0xed, 0xfe}, // MH_CIGAM, defined in mach-o/loader.h
	{0xfe, 0xed, 0xfa, 0xcf}, // MH_MAGIC_64, defined in mach-o/loader.h
	{0xcf, 0xfa, 0xed, 0xfe}, // MH_CIGAM_64, defined in mach-o/loader.h
	{0xca, 0xfe, 0xba, 0xbe}, // FAT_MAGIC, defined in mach-o/fat.h
	{0xbe, 0xba, 0xfe, 0xca}, // FAT_CIGAM, defined in mach-o/fat.h
}

// magicBytes is how many bytes a Mach-O magic is, defined as four.
const magicBytes = 4

// aslrVerb is game aslr: c, xcode or go builds and checks its binaries,
// and nothing checks every Mach-O already in bin/.
func aslrVerb(here repo.Repo, _ config.Config, args []string) error {
	return aslrBy(here.Dir(), args, run, httpFetch, os.Stdout, os.Stderr)
}

// aslrBy is aslrVerb in dir, building with by and fetching with get.
func aslrBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	word, rest, said := verbWord(args)
	switch {
	case !said:
		return aslrBinBy(dir, by, out)
	case word == "c":
		return aslrCBy(dir, rest, by, get, out, errs)
	case word == "xcode":
		return aslrXcodeBy(dir, rest, by, get, out, errs)
	case word == "go":
		return aslrGoBy(dir, rest, by, out)
	}
	return fmt.Errorf("%w, not %q", errASLRLanguage, word)
}

// aslrCBy is game aslr c, as
//
//	game aslr c [-o NAME] [-with LIB] SOURCE...
//
// the sources built as build-c builds them, then their binary checked
// for PIE.
func aslrCBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("aslr c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the program's name; the first source's, by default")
	with := libFlag(fs)
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	program, err := buildCProgram(dir, *name, fs.Args(), *with, by, get)
	if err != nil {
		return err
	}
	return checkPIE(by, dir, program, out)
}

// aslrXcodeBy is game aslr xcode -scheme S [-with LIB]: the scheme built
// as build-xcode builds it, then each Mach-O it left in bin/ checked.
func aslrXcodeBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	if err := buildXcodeBy(dir, args, by, get, out, errs); err != nil {
		return err
	}
	return aslrBinBy(dir, by, out)
}

// aslrGoBy is game aslr go: every cmd/* built into bin/, then checked.
func aslrGoBy(dir string, args []string, by runner, out io.Writer) error {
	if word, _, said := verbWord(args); said {
		return fmt.Errorf("aslr go: %q is a word", word)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return fmt.Errorf("aslr go: no cmd/ directory to build")
	}
	err = os.MkdirAll(filepath.Join(dir, "bin"), folderMode)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		program := filepath.Join("bin", name)
		_, err := by(dir, "go", "build", "-trimpath", "-o", program,
			"./cmd/"+name)
		if err != nil {
			return err
		}
	}
	return aslrBinBy(dir, by, out)
}

type relativePath func(string, string) (string, error)

// aslrBinBy is aslr over every Mach-O already in bin/: each executable
// must be PIE. Nothing in bin/ or no Mach-O in it stops it.
func aslrBinBy(dir string, by runner, out io.Writer) error {
	return aslrBinUsing(dir, by, out, filepath.Rel)
}

// aslrBinUsing is aslrBinBy with the path arithmetic given.
func aslrBinUsing(
	dir string, by runner, out io.Writer, relative relativePath,
) error {
	bins, err := machOBinaries(dir)
	if err != nil {
		return err
	}
	for _, program := range bins {
		rel, err := relative(dir, program)
		if err != nil {
			return err
		}
		if err := checkPIE(by, dir, rel, out); err != nil {
			return err
		}
	}
	return nil
}

// checkPIE reads program's Mach header and refuses it without PIE, or
// says it is.
func checkPIE(by runner, dir, program string, out io.Writer) error {
	said, err := by(dir, otool, "-hv", program)
	if err != nil {
		return err
	}
	ok, err := pieIn(said)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("aslr: %s is not position-independent",
			program)
	}
	fmt.Fprintf(out, "aslr: %s is PIE\n", program)
	return nil
}

// buildCProgram builds the C sources into bin/ as build-c would, and
// gives that binary's path. Sources are checked before anything runs.
func buildCProgram(dir, name string, sources, with []string, by runner,
	get fetcher) (string, error) {
	argv, program, err := cArgv(name, sources)
	if err != nil {
		return "", err
	}
	_, err = linkHardened(dir, argv, program, with, by, get)
	if err != nil {
		return "", err
	}
	return program, nil
}

// machOBinaries is every regular Mach-O under bin/, with any hidden
// folder such as bin/.deps and bin/.xcode left out.
func machOBinaries(dir string) ([]string, error) {
	finding := &findingMachO{bin: filepath.Join(dir, "bin"),
		relative: filepath.Rel}
	err := filepath.WalkDir(finding.bin, finding.visit)
	if errors.Is(err, fs.ErrNotExist) ||
		(err == nil && count.None(len(finding.out))) {
		return nil, errNoBinaries
	}
	return finding.out, err
}

// findingMachO is a walk of bin/ for Mach-O files, and those found.
type findingMachO struct {
	bin      string
	out      []string
	relative relativePath
}

// visit reads one entry: a hidden folder is skipped whole, a hidden
// file is passed, and a Mach-O file is kept.
func (find *findingMachO) visit(path string, entry fs.DirEntry,
	err error) error {
	if err != nil {
		return err
	}
	if path == find.bin {
		return nil
	}
	rel, err := find.relative(find.bin, path)
	if err != nil {
		return err
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
	}
	if entry.IsDir() {
		return nil
	}
	if isMachO(path) {
		find.out = append(find.out, path)
	}
	return nil
}

// isMachO says whether a file begins with a Mach-O magic.
func isMachO(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	var head [magicBytes]byte
	if _, err := io.ReadFull(file, head[:]); err != nil {
		return false
	}
	for _, magic := range machoMagics {
		if bytes.Equal(head[:], magic) {
			return true
		}
	}
	return false
}

// pieIn reads otool -hv's headers and says whether every executable in
// them has PIE. A dylib or a bundle is position-independent and not asked.
func pieIn(hv string) (bool, error) {
	heads, err := machoHeads(hv)
	if err != nil {
		return false, err
	}
	for _, head := range heads {
		if head.filetype == "EXECUTE" && !hasFlag(head.flags, "PIE") {
			return false, nil
		}
	}
	return true, nil
}
