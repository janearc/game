package lint

// The harden rule: what a process may raise, and how it may handle
// files and certificates too loosely. game harden runs this rule alone.
// The .game turns it on with "lint harden".

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/toolchain"
)

// privilege is a syscall that gives a process identities it was not
// started with. syscall and golang.org/x/sys/unix use the same names.
var privilege = map[string]bool{
	"Setuid": true, "Seteuid": true, "Setreuid": true, "Setresuid": true,
	"Setgid": true, "Setegid": true, "Setregid": true, "Setresgid": true,
	"Setgroups": true,
}

// perm says which argument holds the file mode, for each os call that
// takes one. Only os' calls are read. Modes elsewhere are someone
// else's business.
var perm = map[string]int{
	"Chmod": modeAfterName, "Fchmod": modeAfterName,
	"Mkdir": modeAfterName, "MkdirAll": modeAfterName,
	"WriteFile": modeAfterData, "OpenFile": modeAfterData,
}

// where os puts a file mode among a call's arguments. Chmod and Mkdir
// take it after the name. WriteFile and OpenFile take it after the
// name and the data or flag.
const (
	modeAfterName = 1 // defined by os: Chmod(name, mode)
	modeAfterData = 2 // defined by os: WriteFile(name, data, mode)
)

// where exec puts a command's name among its arguments. Command takes
// it first. CommandContext takes it after the context.
const (
	nameFirst        = 0 // defined by exec: Command(name, arg...)
	nameAfterContext = 1 // defined by exec: CommandContext(ctx, name, ...)
)

// the mode bits the harden rule reads, as sys/stat.h defines them.
const (
	setidBits   = 0o6000 // S_ISUID | S_ISGID, defined in sys/stat.h
	othersWrite = 0o022  // S_IWGRP | S_IWOTH, defined in sys/stat.h
)

// noMode is a mode with no bits set.
var noMode int64

// The three things the native rules say. raised: a call gives the
// process a user or group it was not started with. loose: a mode lets
// someone other than the owner write. escalates: a command runs as
// another user.
const (
	raised    = " gives the process privileges it was not started with"
	loose     = "lets someone other than the owner write"
	escalates = "runs a command as another user"
)

// hardened is one harden finding to fix, at a line of rel.
func hardened(rel string, at int, text string) Finding {
	return Finding{rel, at, "harden", text, false}
}

// Hardened runs the harden rule alone over the files dir tracks or
// holds. Those are go and C and Objective-C and Swift. It gives its
// findings and asks nothing else.
func Hardened(dir string) ([]Finding, error) {
	files, err := tracked(dir)
	if err != nil {
		files, err = walked(dir)
		if err != nil {
			return nil, err
		}
	}
	var out []Finding
	for _, rel := range files {
		if testy(rel) {
			continue
		}
		var found []Finding
		var err error
		switch {
		case strings.HasSuffix(rel, ".go"):
			found, err = hardenFile(filepath.Join(dir, rel), rel)
		case nativeSource(rel):
			found, err = hardenSource(filepath.Join(dir, rel), rel)
		default:
			continue
		}
		if err != nil {
			built := runtime.Version()
			return nil, toolchain.Explain(dir, built, err)
		}
		out = append(out, found...)
	}
	return out, nil
}

// nativeSource says whether harden reads rel as C, Objective-C or Swift.
func nativeSource(rel string) bool {
	switch {
	case strings.HasSuffix(rel, ".c"), strings.HasSuffix(rel, ".m"),
		strings.HasSuffix(rel, ".mm"), strings.HasSuffix(rel, ".swift"):
		return true
	}
	return false
}

// hardenFile lints one go file for the harden rule, less the findings on
// a line that says noqa.
func hardenFile(path, rel string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return exempt(src, harden(fset, file, rel)), nil
}

// harden finds what one go file does that raises privilege, writes too
// loosely or skips a check it should not.
func harden(fset *token.FileSet, file *ast.File, rel string) []Finding {
	seen := &hardening{fset: fset, rel: rel}
	ast.Inspect(file, seen.visit)
	return seen.out
}

// hardening is one go file's walk under the harden rule, keeping what
// it found.
type hardening struct {
	fset *token.FileSet
	rel  string
	out  []Finding
}

// visit reads one node. Calls may raise privilege or write too
// loosely. tls.Config literals may skip their check.
func (seen *hardening) visit(node ast.Node) bool {
	switch found := node.(type) {
	case *ast.CallExpr:
		seen.out = append(seen.out,
			hardenCall(seen.fset, found, seen.rel)...)
	case *ast.CompositeLit:
		seen.out = append(seen.out,
			hardenTLS(seen.fset, found, seen.rel)...)
	}
	return true
}

// hardenCall finds the calls that raise privilege or open a file too
// widely.
func hardenCall(fset *token.FileSet, call *ast.CallExpr, rel string) []Finding {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	at := fset.Position(call.Pos()).Line
	if (pkg.Name == "syscall" || pkg.Name == "unix") &&
		privilege[sel.Sel.Name] {
		return []Finding{hardened(rel, at,
			pkg.Name+"."+sel.Sel.Name+raised)}
	}
	if pkg.Name == "exec" && (sel.Sel.Name == "Command" ||
		sel.Sel.Name == "CommandContext") {
		name := nameFirst
		if sel.Sel.Name == "CommandContext" {
			name = nameAfterContext
		}
		if got, ok := strLit(call.Args, name); ok &&
			(got == "sudo" || got == "su") {
			return []Finding{hardened(rel, at, "exec."+sel.Sel.Name+
				"("+strconv.Quote(got)+") "+escalates)}
		}
	}
	if arg, ok := perm[sel.Sel.Name]; ok && pkg.Name == "os" {
		return hardenPerm(call, sel.Sel.Name, arg, rel, at)
	}
	return nil
}

// hardenPerm finds a file mode that sets the setuid or setgid bit, or
// one that lets a group or everyone write.
func hardenPerm(call *ast.CallExpr, name string, arg int, rel string,
	at int) []Finding {
	if len(call.Args) <= arg {
		return nil
	}
	mode, ok := intLit(call.Args[arg])
	if !ok {
		return nil
	}
	switch {
	case mode&setidBits != noMode && (name == "Chmod" || name == "Fchmod"):
		return []Finding{hardened(rel, at,
			"os."+name+" sets the setuid or setgid bit")}
	case mode&othersWrite != noMode:
		return []Finding{hardened(rel, at, "os."+name+" "+loose)}
	}
	return nil
}

// hardenTLS finds a tls.Config that skips the certificate check.
func hardenTLS(fset *token.FileSet, lit *ast.CompositeLit,
	rel string) []Finding {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "tls" || sel.Sel.Name != "Config" {
		return nil
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "InsecureSkipVerify" {
			continue
		}
		if id, ok := kv.Value.(*ast.Ident); ok && id.Name == "true" {
			at := fset.Position(kv.Pos()).Line
			return []Finding{hardened(rel, at,
				"tls.Config skips the certificate check")}
		}
	}
	return nil
}

// strLit is the string a literal holds, or false when it is not a plain
// string.
func strLit(args []ast.Expr, at int) (string, bool) {
	if at >= len(args) {
		return "", false
	}
	lit, ok := args[at].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	held, err := strconv.Unquote(lit.Value)
	return held, err == nil
}

// how strconv reads a number. The base comes from the prefix, 0o or 0x
// or none. C writes a mode in octal. int64 fits a file mode.
const (
	prefixed = 0  // defined by strconv: the base from the prefix
	octal    = 8  // defined: the base C writes a mode in
	modeBits = 64 // defined by go: an int64, which a mode fits
)

// intLit is the integer a literal holds, or false when it is not a plain
// integer.
func intLit(expr ast.Expr) (int64, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return noMode, false
	}
	held, err := strconv.ParseInt(lit.Value, prefixed, modeBits)
	return held, err == nil
}

// The C, Objective-C and Swift side of harden is read line by line.
// game bundles no parser for those languages. Each pattern requires a
// call or a permission, so a word in passing is not a finding.

// cPrivilege matches the calls that give a process a user or group it was
// not started with.
var cPrivilege = regexp.MustCompile(
	`\b(?P<call>setuid|seteuid|setreuid|setresuid|setgid|setegid|` +
		`setregid|setresgid|setgroups)\s*\(`,
)

// cChown matches the calls that move a file to another owner.
var cChown = regexp.MustCompile(`\b(chown|fchown|lchown)\s*\(`)

// cEscalate matches a shell-out that runs sudo or su.
var cEscalate = regexp.MustCompile(
	`\b(system|execlp?|execle|execvp?e?)\s*\([^)]*"` +
		`((/usr)?/bin/)?(sudo|su)\b`,
)

// cSetid matches a chmod that sets the setuid or setgid bit.
var cSetid = regexp.MustCompile(`\b(chmod|fchmod)\s*\([^)]*\b0o?[24][0-7]{3}\b`)

// cFileMode matches a call that takes a file mode. Its octal literal is
// then checked for a write looser than the owner's.
var cFileMode = regexp.MustCompile(`\b(open|creat|mkdir|chmod|fchmod)\s*\(`)

// cOctal is a permission written as octal, such as 0666 or 0o755.
var cOctal = regexp.MustCompile(`\b0o?[0-7]{3,4}\b`)

// cAnyRoot matches the constant that lets a stream accept any certificate.
var cAnyRoot = regexp.MustCompile(`kCFStreamSSLAllowsAnyRoot`)

// swiftPerm matches a Swift FileManager permission written loosely.
var swiftPerm = regexp.MustCompile(
	`\.posixPermissions\s*:\s*(?P<mode>0o[0-7]{3,4})`)

// swiftEscalate matches a Swift Process that names sudo or su.
var swiftEscalate = regexp.MustCompile(
	`\b(launchPath|executableURL)\s*=\s*"[^"]*[/]?(sudo|su)"`,
)

// hardenSource lints one C, Objective-C or Swift file for the harden rule,
// less the findings on a line that says noqa.
func hardenSource(path, rel string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	swift := strings.HasSuffix(rel, ".swift")
	lines := strings.Split(string(src), "\n")
	var out []Finding
	for index, line := range lines {
		if notCode(line) {
			continue
		}
		at := index + 1 // fencepost, defined: lines count from one
		out = append(out, hardenCLine(rel, at, line)...)
		if swift {
			out = append(out, hardenSwiftLine(rel, at, line)...)
		}
	}
	return exempt(src, out), nil
}

// notCode says whether a line is a comment or a directive, where nothing
// a process does is written.
func notCode(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "//") ||
		strings.HasPrefix(trimmed, "/*") ||
		strings.HasPrefix(trimmed, "*") ||
		strings.HasPrefix(trimmed, "#")
}

// every is the count regexp has defined as all the matches there are.
const every = -1

// hardenCLine finds what one C or Objective-C line does that raises
// privilege, trusts any root or opens a file too widely.
func hardenCLine(rel string, at int, line string) []Finding {
	switch {
	case cPrivilege.MatchString(line):
		call := cPrivilege.SubexpIndex("call")
		match := cPrivilege.FindStringSubmatch(line)
		return []Finding{hardened(rel, at, match[call]+raised)}
	case cChown.MatchString(line):
		return []Finding{hardened(rel, at,
			"chown moves a file to another owner")}
	case cEscalate.MatchString(line):
		return []Finding{hardened(rel, at, escalates)}
	case cSetid.MatchString(line):
		return []Finding{hardened(rel, at,
			"chmod sets the setuid or setgid bit")}
	case cAnyRoot.MatchString(line):
		return []Finding{hardened(rel, at,
			"accepts any certificate any root signs")}
	case cFileMode.MatchString(line):
		for _, literal := range cOctal.FindAllString(line, every) {
			if mode, ok := octalOf(literal); ok && looseMode(mode) {
				return []Finding{hardened(rel, at, loose)}
			}
		}
	}
	return nil
}

// hardenSwiftLine finds what one Swift line does beyond the C calls: a
// permission written loosely, or a Process that names sudo or su.
func hardenSwiftLine(rel string, at int, line string) []Finding {
	var out []Finding
	if match := swiftPerm.FindStringSubmatch(line); match != nil {
		literal := match[swiftPerm.SubexpIndex("mode")]
		if mode, ok := octalOf(literal); ok && looseMode(mode) {
			out = append(out, hardened(rel, at, loose))
		}
	}
	if swiftEscalate.MatchString(line) {
		out = append(out, hardened(rel, at, escalates))
	}
	return out
}

// octalOf is the integer an octal literal holds, or false when it is not
// octal digits.
func octalOf(literal string) (int64, bool) {
	digits := strings.TrimPrefix(literal, "0o")
	digits = strings.TrimPrefix(digits, "0")
	mode, err := strconv.ParseInt(digits, octal, modeBits)
	return mode, err == nil
}

// looseMode says a mode lets someone other than the owner write.
func looseMode(mode int64) bool {
	return mode&othersWrite != noMode
}
