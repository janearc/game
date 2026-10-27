package stack_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

// layer lays the count of patches the member's name claims.
type layer map[string]int

// lay answers the count the member's name claims.
func (laid layer) lay(asked stack.Member, _ string) (int, error) {
	return laid[asked.Name()], nil
}

// a member's laying says how many patches it laid, one or many, and
// says nothing when it laid none.
func TestAMemberSaysHowManyPatchesItLaid(t *testing.T) {
	dir := t.TempDir()
	ok := `echo '{"step":"check","state":"ok"}'` + "\n"
	laid := layer{
		"none": count.Nothing,
		"one":  count.One,
		"two":  count.Next(count.One),
	}
	urls := roads{}
	var members []stack.Member
	for name := range laid {
		urls[name] = bareRepo(t, dir, name,
			map[string]string{"build.sh": ok})
		members = append(members, stack.Named(name, "v1"))
	}
	var out bytes.Buffer
	options := made(members, filepath.Join(dir, "work"), child).
		From(urls.at).
		Patching(laid.lay).
		InParallel(len(members)).
		Trying(count.One).
		Saying(&out)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Result() != "ok" {
			t.Errorf("%s: %+v", row.Name(), row)
		}
	}
	said := out.String()
	for _, want := range []string{
		"stack one: 1 patch laid\n",
		"stack two: 2 patches laid\n",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("want %q in:\n%s", want, said)
		}
	}
	// every member but the one that laid none says so
	if strings.Contains(said, "stack none: 0") ||
		strings.Count(said, "laid") != count.Last(len(laid)) {
		t.Errorf("a member that laid none said so:\n%s", said)
	}
}

// noFixPatches lays none, and refuses the fix's.
func noFixPatches(_ stack.Member, dir string) (int, error) {
	if strings.HasSuffix(dir, "-fix") {
		return count.Nothing, errors.New("01.patch does not apply")
	}
	return count.Nothing, nil
}

// fixBuilt notes whether the fix's directory was built.
type fixBuilt struct{ seen bool }

// child notes a build of the fix, then builds as usual.
func (seen *fixBuilt) child(dir string) *exec.Cmd {
	if strings.HasSuffix(dir, "-fix") {
		seen.seen = true
	}
	return child(dir)
}

// a fix whose patches do not land on its clone is not built, and the
// member stays failed.
func TestAFixWhosePatchesDoNotLandIsNotBuilt(t *testing.T) {
	dir := t.TempDir()
	build := `if [ -e FIXED ]; then echo '{"step":"check","state":"ok"}'; ` +
		`else echo '{"step":"build","state":"fail",` +
		`"kind":"deterministic"}'; exit 1; fi` + "\n"
	url := bareRepo(t, dir, "fixme", map[string]string{"build.sh": build})
	builtFix := &fixBuilt{}
	members := []stack.Member{stack.Named("fixme", "v1")}
	options := made(members, filepath.Join(dir, "work"),
		builtFix.child).
		From(roads{"fixme": url}.at).
		Patching(noFixPatches).
		Fixing(selective{}).
		Trying(count.One)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	fixme, _ := rowOf(rows, "fixme")
	if fixme.Result() != "failed" {
		t.Errorf("a fix whose patches failed was called: %+v", fixme)
	}
	if builtFix.seen {
		t.Error("a fix whose patches did not land was built")
	}
	fixDir := filepath.Join(dir, "work", "fixme-fix")
	if _, err := os.Stat(fixDir); err != nil {
		t.Errorf("the fix was never cloned, so its patches were "+
			"never tried: %v", err)
	}
}
