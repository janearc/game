package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
)

// moduleFixture is a module with a library, a passing test and one
// command, which is what verify expects to find under a member. files
// given replace the fixture's own.
func moduleFixture(t *testing.T, files map[string]string) repo.Repo {
	t.Helper()
	dir := t.TempDir()
	base := map[string]string{
		"go.mod": "module example.com/v\n\ngo 1.26\n",
		"v.go": "package v\n\n// Hi greets.\n" +
			"func Hi() string { return \"hi\" }\n",
		"v_test.go": "package v\n\nimport \"testing\"\n\n" +
			"func TestHi(t *testing.T) {\n\tif Hi() != \"hi\" {\n" +
			"\t\tt.Fatal(Hi())\n\t}\n}\n",
		"cmd/hello/main.go": "package main\n\nimport \"example.com/v\"\n\n" +
			"func main() { println(v.Hi()) }\n",
	}
	for name, body := range files {
		base[name] = body
	}
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	for name, body := range base {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return repoAt(dir)
}

// events reads the JSON lines verify wrote, each as "step state", with
// the count or the kind where there is one. other lines are not events.
func events(out string) []string {
	var got []string
	for _, line := range strings.Split(out, "\n") {
		var ev stack.Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		said := ev.Step + " " + ev.State
		if count.Positive(ev.Count) {
			said += fmt.Sprintf(" %d", ev.Count)
		}
		if ev.State == "fail" {
			said += " " + ev.Kind
		}
		got = append(got, said)
	}
	return got
}

// verify tells each step as it starts and passes, runs the test targets
// between the check and the build, then tells the lint count: here one,
// the command's main having no comment.
func TestVerifyTellsEachStepAndTheLintCount(t *testing.T) {
	t.Parallel()
	tree := moduleFixture(t, nil)
	cfg := gamed(t, "lint comments", "target test-smoke "+tree.Dir()+" :: true",
		"target docs "+tree.Dir()+" :: false")
	var out bytes.Buffer
	if err := verify(tree, cfg, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	want := "check start|check ok|test start|test ok|" +
		"build start|build ok|lint ok 1"
	said := events(out.String())
	if got := strings.Join(said, "|"); got != want {
		t.Errorf("got  %s\nwant %s\n%s", got, want, out.String())
	}
}

// a failing test is told as the test step failing, deterministic, and
// verify stops there; a file gofmt would change fails the check step.
func TestVerifyTellsTheFailingStep(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ file, body, want string }{
		"a failing test": {
			"v_test.go",
			"package v\n\nimport \"testing\"\n\n" +
				"func TestHi(t *testing.T) {\n\tt.Fatal(\"no\")\n}\n",
			"test fail deterministic",
		},
		"a file gofmt would change": {
			"v.go",
			"package v\n\nfunc  Hi() string { return \"hi\" }\n",
			"check fail deterministic",
		},
	}
	for name, each := range cases {
		tree := moduleFixture(t, map[string]string{each.file: each.body})
		var out bytes.Buffer
		err := verify(tree, blankConfig(), &out)
		got := events(out.String())
		second := count.Next(count.First)
		if err == nil || len(got) != count.Next(count.One) ||
			got[second] != each.want {
			t.Errorf("%s: err %v, events %v", name, err, got)
		}
	}
}

// a member with no module and no commands is only linted, and says so
// with a count of nothing.
func TestVerifyOnlyLintsWhatHasNoModule(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	page := filepath.Join(dir, "README.md")
	if err := os.WriteFile(page, []byte("# x\n"), openFile); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := verify(repoAt(dir), blankConfig(), &out)
	said := events(out.String())
	if got := strings.Join(said, "|"); err != nil || got != "lint ok" {
		t.Errorf("err %v, events %q", err, got)
	}
}
