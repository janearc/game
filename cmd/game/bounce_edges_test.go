package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// bounced is a repository whose second commit adds a line naming Ada in
// the third person, and has one more such line staged.
func bounced(t *testing.T) repo.Repo {
	t.Helper()
	work := freshTree(t, "x",
		map[string]string{"a.txt": "a\n"})
	commitFiles(t, work.Dir(), map[string]string{"b.txt": "// Ada's call\n"})
	write(t, work.Path("c.txt"), "// as Ada asked\n")
	gitAt(t, work.Dir(), "add", "c.txt")
	return work
}

// bounce reads what is staged unless given a range, says each line it
// holds, and refuses them unless signed off.
func TestBounceHoldsWhatNamesTheAuthor(t *testing.T) {
	work := bounced(t)
	cfg := dotted(t, "author Ada")
	for _, each := range []struct {
		args []string
		line string
	}{
		{nil, "  +// as Ada asked\n"},
		{[]string{"HEAD~1..HEAD"}, "  +// Ada's call\n"},
	} {
		var out bytes.Buffer
		t.Setenv("BOUNCE_OK", "")
		err := verbTo(t, "bounce", bounceVerb, work, cfg, each.args, &out)
		want := "bounce: the author in the third person, in added " +
			"lines:\n" + each.line
		if err == nil || !strings.HasPrefix(err.Error(), "refused.") ||
			out.String() != want {
			t.Errorf("%v: %v, said %q", each.args, err, out.String())
		}
		out.Reset()
		t.Setenv("BOUNCE_OK", "1")
		err = verbTo(t, "bounce", bounceVerb, work, cfg, each.args, &out)
		if err != nil ||
			out.String() != want+"bounce: signed off (BOUNCE_OK); "+
				"passing\n" {
			t.Errorf("%v signed: %v, said %q", each.args, err,
				out.String())
		}
	}
}

// with nothing that names the author, bounce passes and says nothing;
// with no author it cannot look; a range git cannot read is the error.
func TestBouncePassesOrSaysWhyNot(t *testing.T) {
	work := bounced(t)
	var out bytes.Buffer
	t.Setenv("BOUNCE_OK", "")
	if err := verbTo(t, "bounce", bounceVerb, work, dotted(t, "author Grace"), nil,
		&out); err != nil || count.Positive(out.Len()) {
		t.Errorf("nobody named: %v, said %q", err, out.String())
	}
	t.Setenv("BOUNCE_OK", "1")
	err := verbTo(t, "bounce", bounceVerb, work, blankConfig(), nil, quiet)
	if err == nil || !strings.HasPrefix(err.Error(),
		"bounce needs an author") {
		t.Errorf("no author: %v", err)
	}
	err = verbTo(t, "bounce", bounceVerb, work, dotted(t, "author Ada"),
		[]string{"nowhere..HEAD"}, quiet)
	if err == nil {
		t.Error("a range git cannot read passed")
	}
}

// game bounce is signed off by BOUNCE_OK in its environment, and only
// by that.
func TestBounceIsSignedOffByBounceOK(t *testing.T) {
	work := bounced(t)
	cfg := dotted(t, "author Ada")
	t.Setenv("BOUNCE_OK", "")
	if err := viaPzm(t, "bounce", bounceVerb, work, cfg, nil); err == nil {
		t.Error("passed with no BOUNCE_OK")
	}
	t.Setenv("BOUNCE_OK", "1")
	if err := viaPzm(t, "bounce", bounceVerb, work, cfg, nil); err != nil {
		t.Errorf("BOUNCE_OK=1: %v", err)
	}
}
