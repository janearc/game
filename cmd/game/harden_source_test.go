package main

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

func TestSemgrepIssuesReadTheReport(t *testing.T) {
	t.Parallel()
	report := `{"results":[{"check_id":"swift.lang.correctness",` +
		`"path":` + `"` + t.TempDir() + `/A.swift"` + "," +
		`"start":{"line":7,"col":1},"extra":{"severity":"WARNING",` +
		`"message":"a tap wide open"}}]}`
	got := semgrepIssues(report, t.TempDir())
	if len(got) != count.One {
		t.Fatalf("got %v", got)
	}
	line := int(held(t, "semgrep-finding-line"))
	if lineOf(got[count.First]) != line ||
		!strings.Contains(got[count.First].Text(),
			"swift.lang.correctness a tap wide open") {
		t.Errorf("finding: %+v", got[count.First])
	}
	if got := semgrepIssues("not json", t.TempDir()); len(got) != count.One ||
		got[count.First].File() != "semgrep" {
		t.Errorf("junk: %v", got)
	}
}

func TestDiagFindingsReadTheAnalyzer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	said := dir + "/a.m:12:5: warning: use after free [clang.Analyzer]\n"
	got := diagFindings(said, dir)
	if len(got) != count.One || got[count.First].File() != "a.m" ||
		lineOf(got[count.First]) != int(held(t, "diag-finding-line")) ||
		!strings.Contains(got[count.First].Text(), "warning: use after free") {
		t.Errorf("got %+v", got)
	}
	if got := diagFindings("no diagnostics here\n", dir); count.Positive(len(got)) {
		t.Errorf("clean: %v", got)
	}
}
