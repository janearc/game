package repo_test

import (
	"github.com/janearc/game/internal/gittest"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
	"pgregory.net/rapid"
)

// TestElsewhereReadsAUrlAsGitDoes: any scheme but file is another
// machine. So are host and path, and a transport helper. Paths are not,
// even with a colon after the first slash.
func TestElsewhereReadsAUrlAsGitDoes(t *testing.T) {
	t.Parallel()
	for url, want := range map[string]bool{
		"https://example.invalid/x.git":  true,
		"http://example.invalid/x.git":   true,
		"ssh://git@example.invalid/x":    true,
		"git://example.invalid/x":        true,
		"ftp://example.invalid/x":        true,
		"git+ssh://example.invalid/x":    true,
		"FILE:///x":                      true,
		"git@example.invalid:x.git":      true,
		"example.invalid:x.git":          true,
		"example.invalid:":               true,
		"ext::sh -c true":                true,
		"hg::https://example.invalid/x":  true,
		":x":                             true,
		"file:///dists/x.git":            false,
		"/dists/x.git":                   false,
		"dists/x.git":                    false,
		"x.git":                          false,
		"":                               false,
		"dists/at:noon.git":              false,
		"./at:noon.git":                  false,
		"dists/me@" + "home.git":         false,
		"dists/x://y":                    false,
		"/dists/https://example.invalid": false,
	} {
		if got := repo.Elsewhere(url); got != want {
			t.Errorf("Elsewhere(%q) = %v, want %v", url, got, want)
		}
	}
}

// aPathUnderASlash is one drawn path that begins with a slash or a dot.
func aPathUnderASlash(draw *rapid.T) {
	lead := rapid.SampledFrom([]string{"/", "./", "../"}).Draw(draw, "lead")
	rest := rapid.StringMatching(`[a-z0-9:@./ _-]{0,30}`).Draw(draw, "rest")
	if repo.Elsewhere(lead + rest) {
		draw.Fatalf("%q was read as another machine", lead+rest)
	}
}

// TestElsewhereAnyPathUnderASlashIsHere: whatever follows a leading
// slash or dot-slash, a colon among it does not make the path another
// machine.
func TestElsewhereAnyPathUnderASlashIsHere(t *testing.T) {
	t.Parallel()
	rapid.Check(t, aPathUnderASlash)
}

// aHostAndPath is one drawn host and path. the user may be left out.
func aHostAndPath(draw *rapid.T) {
	user := rapid.SampledFrom([]string{"", "git@", "me@"}).Draw(draw, "user")
	host := rapid.StringMatching(`[a-z][a-z0-9.-]{0,12}`).Draw(draw, "host")
	path := rapid.StringMatching(`[a-z0-9:@./_-]{0,30}`).Draw(draw, "path")
	url := user + host + ":" + path
	if strings.HasPrefix(url, "file://") {
		draw.Skip("a file url is on this machine")
	}
	if !repo.Elsewhere(url) {
		draw.Fatalf("%q was read as this machine", url)
	}
}

// TestElsewhereAnyHostAndPathIsElsewhere: a host with no slash in it and
// then a colon is another machine. The path after it does not matter.
func TestElsewhereAnyHostAndPathIsElsewhere(t *testing.T) {
	t.Parallel()
	rapid.Check(t, aHostAndPath)
}

// aClean is a repository with no config but what a test gives it.
func aClean(t *testing.T) (repo.Repo, *gittest.Mem) {
	t.Helper()
	return fixture(t)
}

// TestLeavesAsksGitWhereAPushGoes: git is asked, not the spelling. Paths
// stay here. So do remotes whose url is a path. Remotes whose push
// url is elsewhere leave. So do remotes whose url is elsewhere. So do
// paths that a rewrite sends to a url.
func TestLeavesAsksGitWhereAPushGoes(t *testing.T) {
	here, mem := aClean(t)
	mem.Remote("near", "/dists/near.git", nil)
	mem.Remote("far", "https://example.invalid/far.git", nil)
	mem.Remote("split", "example.invalid:split.git", nil)
	mem.Config("url.https://example.invalid/.insteadOf",
		"/mirrored/")
	mem.Config("url.example.invalid:.pushInsteadOf", "/pushed/")
	for dist, want := range map[string]bool{
		"/dists/plain.git":          false,
		"near":                      false,
		"far":                       true,
		"split":                     true,
		"/mirrored/x.git":           true,
		"/pushed/x.git":             true,
		"example.invalid:x.git":     true,
		"/elsewhere/mirrored/x.git": false,
	} {
		if got := here.Leaves(dist); got != want {
			t.Errorf("Leaves(%q) = %v, want %v", dist, got, want)
		}
	}
}

// TestLeavesLetsPushInsteadOfBeatInsteadOf: for a push the push rule
// wins, and of two rules the longer prefix wins.
func TestLeavesLetsPushInsteadOfBeatInsteadOf(t *testing.T) {
	here, mem := aClean(t)
	mem.Config("url.example.invalid:.insteadOf", "/a/")
	mem.Config("url./local/.pushInsteadOf", "/a/")
	mem.Config("url./local/deep/.insteadOf", "/b/c/")
	mem.Config("url.example.invalid:.insteadOf", "/b/")
	for dist, want := range map[string]bool{
		"/a/x.git":   false,
		"/b/c/x.git": false,
		"/b/x.git":   true,
	} {
		if got := here.Leaves(dist); got != want {
			t.Errorf("Leaves(%q) = %v, want %v", dist, got, want)
		}
	}
}

// TestLeavesIsYesWhenGitCannotSay: a repository that is not there cannot
// be asked, and a push that cannot be placed is taken to leave.
func TestLeavesIsYesWhenGitCannotSay(t *testing.T) {
	gone := repo.At("", repo.Exec())
	if !gone.Leaves("/dists/x.git") {
		t.Fatal("a push git could not place was taken to stay here")
	}
}

// TestLeavesTakesTheFirstOfTwoRulesThatAreAsLong: git keeps the first
// rule where two match as far, so the later one does not replace it.
func TestLeavesTakesTheFirstOfTwoRulesThatAreAsLong(t *testing.T) {
	here, mem := aClean(t)
	mem.Config("url.example.invalid:.insteadOf", "/q/")
	mem.Config("url./local/.insteadOf", "/q/")
	if !here.Leaves("/q/x.git") {
		t.Error("the later of two equal rules was applied")
	}
}

// TestLeavesAppliesARuleWithNothingToMatch: a rule that begins every
// url is a rule, and is applied to one that nothing else matches.
func TestLeavesAppliesARuleWithNothingToMatch(t *testing.T) {
	here, mem := aClean(t)
	mem.Config("url.example.invalid:.insteadOf", "")
	if !here.Leaves("/dists/plain.git") {
		t.Error("a rule that begins every url was not applied")
	}
}
