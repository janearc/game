package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// standTools makes a directory of tools that do nothing, each named,
// and makes it the whole of PATH, so lint.Tool finds exactly those and
// no analyzer of this machine's. the go tool is off the path too, so
// its own bin directories are not searched. a test that stands tools
// is not parallel, as PATH is the process's.
func standTools(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		write(t, filepath.Join(dir, name), "#!/bin/sh\nexit 0\n")
		openScript := os.FileMode(0o755)
		if err := os.Chmod(filepath.Join(dir, name), openScript); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("GOBIN", dir)
	t.Setenv("GOPATH", dir)
	return dir
}

func TestGosecIssuesReadTheReport(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	report := `{"Issues":[{"severity":"HIGH","confidence":"HIGH",` +
		`"rule_id":"G104","details":"Errors unhandled.",` +
		`"file":"` + dir + `/x.go","line":"10"}]}`
	got := gosecIssues(report, dir)
	line := int(held(t, "gosec-finding-line"))
	if len(got) != count.One || got[count.First].File() != "x.go" ||
		lineOf(got[count.First]) != line ||
		!strings.Contains(got[count.First].Text(), "G104 Errors unhandled.") {
		t.Errorf("got %v", got)
	}
	junk := gosecIssues("not json", dir)
	if len(junk) != count.One || junk[count.First].File() != "gosec" {
		t.Errorf("junk: %v", junk)
	}
}

// govulnStream is govulncheck -json as it comes: a stream of objects,
// with the vulnerability named once and each reach of it traced, the
// first trace entry the vulnerable symbol and the last the caller here.
const govulnStream = `{"config":{"protocol_version":"v1.0.0"}}
{"progress":{"message":"Scanning your code"}}
{"osv":{"id":"GO-2023-0001","summary":"a tap wide open",
  "affected":[{"package":{"name":"example.com/x"}}]}}
{"finding":{"osv":"GO-2023-0001","trace":[{"module":"example.com/x"}]}}
{"finding":{"osv":"GO-2023-0001","fixed_version":"v1.2.0","trace":[
  {"module":"example.com/x","package":"example.com/x/y",
   "function":"Open","receiver":"Tap"},
  {"module":"here","package":"here/cmd/app","function":"main",
   "position":{"filename":"cmd/app/main.go","line":12,"column":3}}]}}
`

func TestGovulnVulnsReadTheStream(t *testing.T) {
	t.Parallel()
	got := govulnVulns(govulnStream)
	if len(got) != count.One {
		t.Fatalf("got %v", got)
	}
	vuln := got[count.First]
	line := int(held(t, "govuln-caller-line"))
	if vuln.File() != "cmd/app/main.go" || lineOf(vuln) != line {
		t.Errorf("held to %s:%d, not the caller", vuln.File(), lineOf(vuln))
	}
	for _, want := range []string{"GO-2023-0001", "a tap wide open",
		"example.com/x/y.Tap.Open", "fixed in v1.2.0"} {
		if !strings.Contains(vuln.Text(), want) {
			t.Errorf("no %q in %q", want, vuln.Text())
		}
	}
	junk := govulnVulns("not json")
	if len(junk) != count.One || junk[count.First].File() != "govulncheck" {
		t.Errorf("junk: %v", junk)
	}
	if none := govulnVulns(""); count.Positive(len(none)) {
		t.Errorf("nothing said: %v", none)
	}
}

// a reach with no position in this tree is held to the module.
func TestGovulnVulnsFallBackToTheModule(t *testing.T) {
	t.Parallel()
	stream := `{"finding":{"osv":"GO-2023-0002","trace":[` +
		`{"module":"example.com/z","package":"example.com/z",` +
		`"function":"Leak"}]}}` + "\n"
	got := govulnVulns(stream)
	if len(got) != count.One {
		t.Fatalf("got %+v", got)
	}
	vuln := got[count.First]
	if vuln.File() != "example.com/z" || lineOf(vuln) != count.Nothing ||
		!strings.Contains(vuln.Text(), "GO-2023-0002: GO-2023-0002") {
		t.Errorf("got %+v", got)
	}
}

// offline is a runner that keeps the tools it ran and has govulncheck
// fail for want of the network.
type offline struct {
	vuln string
	ran  []string
}

// run is the runner.
func (runs *offline) run(_, name string, _ ...string) (string, error) {
	runs.ran = append(runs.ran, name)
	if name == runs.vuln {
		return "", errors.New("govulncheck: no network")
	}
	return `{"Issues":[]}`, nil
}

// the tools run from where look found them, so a go install not on
// PATH still runs; and a tool that fell over says so.
func TestGoToolsRunFromWhereTheyStand(t *testing.T) {
	stand := standTools(t, "gosec", "govulncheck")
	runs := &offline{vuln: filepath.Join(stand, "govulncheck")}
	dir := t.TempDir()
	if got := gosecFindings(dir, runs.run); count.Positive(len(got)) {
		t.Errorf("gosec: %v", got)
	}
	got := govulnFindings(dir, runs.run)
	if len(got) != count.One ||
		!strings.Contains(got[count.First].Text(), "govulncheck failed") {
		t.Errorf("govulncheck: %v", got)
	}
	want := filepath.Join(stand, "gosec") + " " +
		filepath.Join(stand, "govulncheck")
	if strings.Join(runs.ran, " ") != want {
		t.Errorf("ran %v", runs.ran)
	}
}

func TestMissingToolIsAFinding(t *testing.T) {
	standTools(t) // nothing is standing in
	sec := gosecFindings(t.TempDir(), (&tools{}).run)
	if len(sec) != count.One || sec[count.First].File() != "gosec" ||
		!strings.Contains(sec[count.First].Text(), "did not run") {
		t.Errorf("gosec: %v", sec)
	}
	vuln := govulnFindings(t.TempDir(), (&tools{}).run)
	if len(vuln) != count.One || vuln[count.First].File() != "govulncheck" {
		t.Errorf("govulncheck: %v", vuln)
	}
}

func TestHardenGoWithNoModuleIsNothing(t *testing.T) {
	t.Parallel()
	got, err := hardenGo(t.TempDir(), (&tools{}).run)
	if err != nil || got != nil {
		t.Errorf("%v %v", got, err)
	}
}
