package main

// game harden's Mach-O pass reads every binary in bin/ back with otool
// and codesign. One that can still be told apart from a hardened binary
// is refused.
//
// No PIE is a finding. So is an executable stack. So is no signature.
// So is one segment both writable and executable. So is one weakening
// entitlement.
//
// Native binaries are also held to the hardened runtime flag in the
// signature and to the __RESTRICT segment that seals dyld's
// environment. Go binaries cannot take either and are let off.
//
// The stack canary is not read back. Programs with no protectable
// frame have no canary symbol to show, so its absence proves nothing.
// The flag is build-c's to set.

import (
	"debug/buildinfo"
	"errors"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/lint"
)

// forbiddenEntitlements are keys that weaken the hardened runtime.
// each is a finding if codesign's plist sets it true.
var forbiddenEntitlements = []string{
	"get-task-allow",
	"disable-library-validation",
	"allow-jit",
	"allow-dyld-environment-variables",
	"allow-unsigned-executable-memory",
}

// entitledTrue matches one entitlement key set true in a plist. The key
// is the named group, whatever prefix it carries.
var entitledTrue = regexp.MustCompile(
	`<key>[^<]*?(?P<key>[a-z-]+)</key>\s*<true\s*/>`)

// shell is the program harden uses to merge a tool's streams. One tool
// says everything on stderr.
const shell = "sh"

// machoHead is one Mach-O slice's filetype and flags, read from otool -hv.
type machoHead struct {
	filetype string
	flags    []string
}

// hardenMach audits every Mach-O already in bin/, none being no failure.
func hardenMach(dir string, by runner) ([]lint.Finding, error) {
	return hardenMachUsing(dir, by, filepath.Rel)
}

// hardenMachUsing is hardenMach with the path arithmetic given.
func hardenMachUsing(
	dir string, by runner, relative relativePath,
) ([]lint.Finding, error) {
	bins, err := machOBinaries(dir)
	if err != nil {
		if errors.Is(err, errNoBinaries) {
			return nil, nil
		}
		return nil, err
	}
	var out []lint.Finding
	for _, program := range bins {
		rel, err := relative(dir, program)
		if err != nil {
			return nil, err
		}
		out = append(out, hardenOne(by, dir, rel)...)
	}
	return out, nil
}

// hardenOne audits one Mach-O under dir at rel.
func hardenOne(by runner, dir, rel string) []lint.Finding {
	var out []lint.Finding
	if said, err := by(dir, otool, "-hv", rel); err != nil {
		unread := "otool -hv: " + err.Error()
		out = append(out, hardFinding(rel, unread))
	} else {
		out = append(out, headerFindings(said, rel)...)
	}
	loads := ""
	if said, err := by(dir, otool, "-l", rel); err == nil {
		loads = said
		out = append(out, segmentFindings(said, rel)...)
	}
	ent, err := by(dir, "codesign", "-d", "--entitlements", "-", rel)
	if err != nil {
		out = append(out, hardFinding(rel, "not codesigned"))
	} else {
		out = append(out, entitlementFindings(ent, rel)...)
	}
	// Go binaries cannot take the runtime or dyld restrictions. Only
	// native binaries are held to those.
	if isGoBinary(dir, rel) {
		return out
	}
	if vv, err := signatureOf(by, dir, rel); err == nil {
		out = append(out, runtimeFindings(vv, rel)...)
	}
	if loads != "" {
		out = append(out, restrictFinding(loads, rel)...)
	}
	return out
}

// isGoBinary says whether rel under dir carries Go build info. Every
// binary the go tool links does, and nothing else can.
func isGoBinary(dir, rel string) bool {
	_, err := buildinfo.ReadFile(filepath.Join(dir, rel))
	return err == nil
}

// signatureOf is what codesign says of rel's signature. codesign -d
// writes all of it to stderr, exit 0 or not. The shell folds the
// streams together, since the runner keeps stdout alone.
func signatureOf(by runner, dir, rel string) (string, error) {
	return by(dir, shell, "-c",
		`exec codesign -d -vv -- "$1" 2>&1`, shell, rel)
}

// signFlags matches the CodeDirectory flags codesign -d -vv prints. It
// prints them as flags=0x10002(adhoc,runtime). The words in the
// parentheses are the named group.
var signFlags = regexp.MustCompile(`flags=0x[0-9a-f]+\((?P<words>[^)]*)\)`)

// every is the count regexp has defined as all the matches there are.
const every = -1

// signedWith is the flag words a signature carries, by name.
func signedWith(vv string) map[string]bool {
	words := signFlags.SubexpIndex("words")
	flags := map[string]bool{}
	for _, match := range signFlags.FindAllStringSubmatch(vv, every) {
		for _, word := range strings.Split(match[words], ",") {
			flags[strings.TrimSpace(word)] = true
		}
	}
	return flags
}

// runtimeFindings finds a native binary whose signature is not the
// hardened runtime. adhoc and runtime are not exclusive: an ad hoc
// signature with the runtime flag is hardened and passes.
func runtimeFindings(vv, rel string) []lint.Finding {
	flags := signedWith(vv)
	if flags["runtime"] {
		return nil
	}
	if flags["adhoc"] {
		return []lint.Finding{hardFinding(rel,
			"ad-hoc signed, not the hardened runtime")}
	}
	return []lint.Finding{hardFinding(rel, "not the hardened runtime")}
}

// restrictFinding finds a native binary that has not sealed dyld's
// environment away.
func restrictFinding(otoolL, rel string) []lint.Finding {
	for _, seg := range machoSegments(otoolL) {
		if seg.name == "__RESTRICT" {
			return nil
		}
	}
	return []lint.Finding{hardFinding(rel,
		"dyld environment is not restricted; no __RESTRICT segment")}
}

// headerFindings finds an executable without PIE or a slice that lets the
// stack run.
func headerFindings(hv, rel string) []lint.Finding {
	heads, err := machoHeads(hv)
	if err != nil {
		return []lint.Finding{hardFinding(rel, err.Error())}
	}
	var out []lint.Finding
	for _, head := range heads {
		if head.filetype == "EXECUTE" && !hasFlag(head.flags, "PIE") {
			out = append(out, hardFinding(rel,
				"executable is not position-independent"))
		}
		if hasFlag(head.flags, "ALLOW_STACK_EXECUTION") {
			out = append(out, hardFinding(rel,
				"allows code on the stack"))
		}
	}
	return out
}

// vmProtWrite and vmProtExecute are the bits of a segment's protection
// that let it be written and run. mach/vm_prot.h defines them.
const (
	vmProtWrite   = 0x02 // VM_PROT_WRITE, defined in mach/vm_prot.h
	vmProtExecute = 0x04 // VM_PROT_EXECUTE, defined in mach/vm_prot.h
)

// writable says a segment's protection lets it be written.
func (seg machoSeg) writable() bool {
	return seg.maxprot&vmProtWrite != noBits
}

// executable says a segment's protection lets it run.
func (seg machoSeg) executable() bool {
	return seg.maxprot&vmProtExecute != noBits
}

// noBits is a protection with nothing set.
var noBits uint64

// segmentFindings finds a segment that is both writable and executable.
// No hardened binary ships one.
func segmentFindings(otoolL, rel string) []lint.Finding {
	var out []lint.Finding
	for _, seg := range machoSegments(otoolL) {
		if seg.writable() && seg.executable() {
			out = append(out, hardFinding(rel,
				seg.name+" is writable and executable"))
		}
	}
	return out
}

// entitlementFindings finds the entitlements that weaken the hardened
// runtime, where the plist sets them true. One set false is no finding.
func entitlementFindings(plist, rel string) []lint.Finding {
	key := entitledTrue.SubexpIndex("key")
	granted := map[string]bool{}
	for _, match := range entitledTrue.FindAllStringSubmatch(plist, every) {
		granted[match[key]] = true
	}
	var out []lint.Finding
	for _, name := range forbiddenEntitlements {
		if granted[name] {
			out = append(out, hardFinding(rel, "entitlement "+name+
				" weakens the hardened runtime"))
		}
	}
	return out
}

// otool -hv prints one line per slice under a heading that names the
// columns: magic cputype cpusubtype caps filetype ncmds sizeofcmds
// flags. The filetype is the fifth column. The flags begin at the
// eighth and run to the end of the line.
const (
	magicColumn    = 0 // the magic, first of the columns otool defined
	filetypeColumn = 4 // the filetype, fifth of the columns otool defined
	flagsColumn    = 7 // the first flag, eighth of those otool defined
)

// machoHeads parses otool -hv into its slices.
func machoHeads(hv string) ([]machoHead, error) {
	var out []machoHead
	for _, line := range strings.Split(hv, "\n") {
		columns := strings.Fields(line)
		if len(columns) <= magicColumn ||
			!strings.HasPrefix(columns[magicColumn], "MH_") {
			continue
		}
		if len(columns) <= flagsColumn {
			return nil, errNoHeader
		}
		out = append(out, machoHead{filetype: columns[filetypeColumn],
			flags: columns[flagsColumn:]})
	}
	if out == nil {
		return nil, errNoHeader
	}
	return out, nil
}

// machoSeg is one segment's protections, read from otool -l.
type machoSeg struct {
	name    string
	maxprot uint64
}

// protection reads a maxprot as otool prints it into the bits of a
// vm_prot_t. otool prints 0x00000007 and the prefix says the base.
func protection(said string) uint64 {
	const prefixed = 0 // defined by strconv: the base from the prefix
	const bits = 32    // defined by mach/vm_prot.h: a vm_prot_t is an int
	prot, _ := strconv.ParseUint(said, prefixed, bits)
	return prot
}

// machoSegments parses otool -l into the segments and their max
// protections. The max protection is the strongest of read, write and
// execute. Each line of interest is a key and a value: segname __TEXT
// or maxprot 0x00000005.
func machoSegments(otoolL string) []machoSeg {
	var out []machoSeg
	var cur *machoSeg
	for _, line := range strings.Split(otoolL, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		value = strings.TrimSpace(value)
		switch {
		case key == "segname":
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &machoSeg{name: value}
		case key == "maxprot" && cur != nil:
			cur.maxprot = protection(value)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

// hasFlag says whether a slice's flags hold one word.
func hasFlag(flags []string, word string) bool {
	for _, flag := range flags {
		if flag == word {
			return true
		}
	}
	return false
}
