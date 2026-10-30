package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// htmlProfile is a profile with a file half run.
const htmlProfile = "mode: set\nexample.com/m/a.go:3.1,4.2 2 1\n" +
	"example.com/m/a.go:6.1,7.2 2 0\n"

// cover --html writes the html report next to the profile and says where.
// the report is written even when a file is under 100%, which is when it
// is wanted.
func TestCoverHTMLWritesTheReportAndSaysWhere(t *testing.T) {
	t.Parallel()
	dir := tree(t, map[string]string{"go.mod": goMod})
	var calls []string
	var out bytes.Buffer
	err := argsCoverBy(t, dir, []string{"--html"},
		profileWriter{htmlProfile, &calls}.run, &out)
	if !errors.Is(err, errNotFull) {
		t.Errorf("a file under 100%%: %v", err)
	}
	want := "go tool cover -html=" + goProfile + " -o " + goHTML
	if len(calls) != 2 || calls[1] != want {
		t.Errorf("calls: %q, want the last to be %q", calls, want)
	}
	said := "cover: html report at " + filepath.Join(dir, goHTML) + "\n"
	if !strings.HasSuffix(out.String(), said) {
		t.Errorf("said\n%s\nwant it to end with %q", out.String(), said)
	}
}

// anti-test: without --html there is no html, and with a word there is a
// refusal and nothing runs.
func TestCoverHTMLOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	dir := tree(t, map[string]string{"go.mod": goMod})
	var calls []string
	err := argsCoverBy(t, dir, nil, profileWriter{htmlProfile, &calls}.run,
		&bytes.Buffer{})
	if !errors.Is(err, errNotFull) || len(calls) != 1 {
		t.Errorf("no --html: %v, %q", err, calls)
	}
	calls = nil
	err = argsCoverBy(t, dir, []string{"c", "--html"},
		profileWriter{htmlProfile, &calls}.run, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "takes none") ||
		len(calls) != 0 {
		t.Errorf("c --html: %v, %q", err, calls)
	}
}

// the tests failing is their error, and there is no html of nothing.
func TestCoverHTMLWithFailingTestsWritesNone(t *testing.T) {
	t.Parallel()
	dir := tree(t, map[string]string{"go.mod": goMod})
	var calls []string
	failing := func(_, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", errors.New("exit status 1")
	}
	err := argsCoverBy(t, dir, []string{"--html"}, failing, &bytes.Buffer{})
	if err == nil || len(calls) != 1 {
		t.Errorf("failing tests: %v, %q", err, calls)
	}
}

// the html tool failing is its error.
func TestCoverHTMLToolFailing(t *testing.T) {
	t.Parallel()
	dir := tree(t, map[string]string{"go.mod": goMod})
	var calls []string
	writer := profileWriter{htmlProfile, &calls}
	failing := func(at, name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "tool" {
			return "", errors.New("no cover tool")
		}
		return writer.run(at, name, args...)
	}
	err := argsCoverBy(t, dir, []string{"--html"}, failing, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no cover tool") {
		t.Errorf("the tool failing: %v", err)
	}
}
