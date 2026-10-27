package main

// game harden's Go pass lays gosec over the source rules and checks the
// module's dependencies with govulncheck. Tools that do not run are
// findings by themselves.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/lint"
)

// rule is the name every harden finding is filed under.
const rule = "harden"

// hardFinding is a lint finding for a whole file. no line is set.
func hardFinding(file, text string) lint.Finding {
	return lint.Report(file, noLine, rule, text)
}

// noLine is the line of a finding against a whole file: none.
var noLine int

// hardenGo finds nothing when there is no module here, or else what the
// go analyzers find.
func hardenGo(dir string, by runner) ([]lint.Finding, error) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return append(gosecFindings(dir, by), govulnFindings(dir, by)...), nil
}

// gosecFindings runs gosec, or says it is not here.
func gosecFindings(dir string, by runner) []lint.Finding {
	gosec := lint.Tool("gosec")
	if gosec == "" {
		return missing("gosec", "go install "+
			"github.com/securego/gosec/v2/cmd/gosec@latest")
	}
	said, err := by(dir, gosec, "-no-fail", "-fmt=json", "./...")
	if err != nil {
		return failed("gosec", err)
	}
	return gosecIssues(said, dir)
}

// govulnFindings runs govulncheck, or says it is not here.
func govulnFindings(dir string, by runner) []lint.Finding {
	govulncheck := lint.Tool("govulncheck")
	if govulncheck == "" {
		return missing("govulncheck",
			"go install golang.org/x/vuln/cmd/govulncheck@latest")
	}
	// govulncheck -json exits 0 whether or not it found anything.
	// Nonzero means the tool itself failed, and that is a finding.
	said, err := by(dir, govulncheck, "-json", "./...")
	if err != nil {
		return failed("govulncheck", err)
	}
	return govulnVulns(said)
}

// missing is one tool that is not installed, as a finding.
func missing(tool, how string) []lint.Finding {
	return []lint.Finding{hardFinding(tool, tool+" did not run; "+how)}
}

// failed is one tool that ran and fell over, as a finding that says how.
func failed(tool string, err error) []lint.Finding {
	return []lint.Finding{hardFinding(tool, tool+" failed: "+err.Error())}
}

// gosecIssues reads gosec's JSON report into findings. the file is made
// relative to dir and the line is read.
func gosecIssues(said, dir string) []lint.Finding {
	var report struct {
		Issues []struct {
			Severity   string `json:"severity"`
			Confidence string `json:"confidence"`
			RuleID     string `json:"rule_id"`
			Details    string `json:"details"`
			File       string `json:"file"`
			Line       string `json:"line"`
		} `json:"Issues"`
	}
	if err := json.Unmarshal([]byte(said), &report); err != nil {
		return unreadable("gosec")
	}
	var out []lint.Finding
	for _, issue := range report.Issues {
		line, _ := strconv.Atoi(issue.Line)
		text := issue.RuleID + " " + issue.Details + " (" +
			issue.Severity + " severity, " +
			issue.Confidence + " confidence)"
		file := relTo(dir, issue.File)
		out = append(out, lint.Report(file, line, rule, text))
	}
	return out
}

// relTo is path under dir, or path as it came when it is not under dir.
func relTo(dir, path string) string {
	if !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// govulnPosition is where a frame of govulncheck's trace sits in a
// file. json reads these fields; this is govulncheck's wire.
type govulnPosition struct {
	Filename string `json:"filename"`
	Line     int    `json:"line"`
}

// govulnFrame is one step in govulncheck's trace. the first is the
// vulnerable symbol and the last is the caller in this tree. json reads
// these fields.
type govulnFrame struct {
	Module   string          `json:"module"`
	Package  string          `json:"package"`
	Function string          `json:"function"`
	Receiver string          `json:"receiver"`
	Position *govulnPosition `json:"position"`
}

// called is the symbol a frame names, with its receiver when it has one.
func (frame govulnFrame) called() string {
	if frame.Receiver == "" {
		return frame.Package + "." + frame.Function
	}
	return frame.Package + "." + frame.Receiver + "." + frame.Function
}

// govulnReach is one govulncheck finding: a vulnerability and the trace
// by which the module reaches it. json reads these fields; this is
// govulncheck's wire.
type govulnReach struct {
	OSV   string        `json:"osv"`
	Fixed string        `json:"fixed_version"`
	Trace []govulnFrame `json:"trace"`
}

// reached says whether the trace reaches a function. an import that
// stops at a module or package never calls into it.
func (reach govulnReach) reached() bool {
	return len(reach.Trace) != noFrames && reach.sink().Function != ""
}

// noFrames is the length of a trace with nothing in it.
var noFrames int

// sink is the vulnerable symbol, which heads the trace.
func (reach govulnReach) sink() govulnFrame {
	head := 0 // defined by govulncheck: the vulnerable symbol is first
	return reach.Trace[head]
}

// caller is the frame in this tree, which ends the trace.
func (reach govulnReach) caller() govulnFrame {
	last := len(reach.Trace) - 1 // fencepost, defined: the nth is at n-1
	return reach.Trace[last]
}

// govulnOSV is a vulnerability record from govulncheck. json reads
// these fields; this is the wire format.
type govulnOSV struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// govulnEntry is one object in govulncheck -json's stream. It is an
// osv record naming a vulnerability or a finding tracing a call to one.
// Config and progress objects are read past. json reads these fields;
// this is govulncheck's wire.
type govulnEntry struct {
	OSV     *govulnOSV   `json:"osv"`
	Finding *govulnReach `json:"finding"`
}

// unreadable is a tool's report game could not read, as a finding.
func unreadable(tool string) []lint.Finding {
	return []lint.Finding{hardFinding(tool,
		tool+"'s report was not readable")}
}

// govulnVulns reads govulncheck -json into one finding per vulnerable
// symbol the module reaches. The output is a stream of objects, not one
// document.
//
// Findings whose trace names no function are imports that never call
// the vulnerable code. govulncheck reports those as informational and
// they are not held against the tree.
func govulnVulns(said string) []lint.Finding {
	summary := map[string]string{}
	var out []lint.Finding
	dec := json.NewDecoder(strings.NewReader(said))
	for {
		var entry govulnEntry
		err := dec.Decode(&entry)
		if err == io.EOF {
			break
		}
		if err != nil {
			return unreadable("govulncheck")
		}
		if osv := entry.OSV; osv != nil {
			summary[osv.ID] = strings.TrimSpace(osv.Summary)
		}
		if reach := entry.Finding; reach != nil && reach.reached() {
			out = append(out, govulnFinding(*reach, summary))
		}
	}
	return out
}

// govulnFinding is one reached vulnerability as a finding. It sits at
// the call in this tree when the trace ends in one, else at the module.
func govulnFinding(reach govulnReach, summary map[string]string) lint.Finding {
	what := summary[reach.OSV]
	if what == "" {
		what = reach.OSV
	}
	text := reach.OSV + ": " + what + "; calls " + reach.sink().called()
	if reach.Fixed != "" {
		text += ", fixed in " + reach.Fixed
	}
	caller := reach.caller()
	if caller.Position != nil && caller.Position.Filename != "" {
		return lint.Report(caller.Position.Filename,
			caller.Position.Line, rule, text)
	}
	return hardFinding(reach.sink().Module, text)
}
