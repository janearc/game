package main

// game harden's source analyzers beyond the go pass. semgrep runs over
// whatever is here in whatever language. The clang diagnostics from
// xcodebuild analyze are read back into findings.

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/lint"
)

// semgrepFindings runs semgrep over the tree, or says it is not here.
func semgrepFindings(dir string, by runner) []lint.Finding {
	semgrep := lint.Tool("semgrep")
	if semgrep == "" {
		return missing("semgrep", "brew install semgrep")
	}
	said, err := by(dir, semgrep, "--json", "--config", "auto", ".")
	if err != nil {
		return failed("semgrep", err)
	}
	return semgrepIssues(said, dir)
}

// semgrepIssues reads semgrep's JSON results into findings.
func semgrepIssues(said, dir string) []lint.Finding {
	var report struct {
		Results []struct {
			CheckID string `json:"check_id"`
			Path    string `json:"path"`
			Start   struct {
				Line int `json:"line"`
			} `json:"start"`
			Extra struct {
				Message  string `json:"message"`
				Severity string `json:"severity"`
			} `json:"extra"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(said), &report); err != nil {
		return unreadable("semgrep")
	}
	var out []lint.Finding
	for _, result := range report.Results {
		rel, _ := filepath.Rel(dir, result.Path)
		text := strings.TrimSpace(result.Extra.Message)
		if text == "" {
			text = result.CheckID
		}
		severity := result.Extra.Severity
		if severity == "" {
			severity = "unknown severity"
		}
		out = append(out, lint.Report(rel, result.Start.Line, rule,
			result.CheckID+" "+text+" ("+severity+")"))
	}
	return out
}

// clangDiag matches one clang diagnostic: path:line:col: kind: message.
var clangDiag = regexp.MustCompile(`^(?P<file>[^:]+):(?P<line>[0-9]+)` +
	`:[0-9]+: (?P<kind>warning|error): (?P<message>.*)$`)

// diagFindings reads clang diagnostics into findings. file is made
// relative to dir.
func diagFindings(said, dir string) []lint.Finding {
	file := clangDiag.SubexpIndex("file")
	at := clangDiag.SubexpIndex("line")
	kind := clangDiag.SubexpIndex("kind")
	message := clangDiag.SubexpIndex("message")
	var out []lint.Finding
	for _, said := range strings.Split(said, "\n") {
		match := clangDiag.FindStringSubmatch(strings.TrimSpace(said))
		if match == nil {
			continue
		}
		rel, _ := filepath.Rel(dir, match[file])
		line, _ := strconv.Atoi(match[at])
		out = append(out, lint.Report(rel, line, rule,
			match[kind]+": "+match[message]))
	}
	return out
}
