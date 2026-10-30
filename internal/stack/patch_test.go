package stack_test

import (
	"bytes"
	"context"
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
