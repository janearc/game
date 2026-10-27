package lint_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// ranOn lints the files, name to text, as Run does in a directory of
// their own that is no repository, and gives the findings less
// staticcheck's, which is not about the files.
func ranOn(
	t *testing.T, linter lint.Lint, files map[string]string,
) []lint.Finding {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeIn(t, dir, name, body)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []lint.Finding
	for _, one := range found {
		if one.Rule() != "staticcheck" {
			out = append(out, one)
		}
	}
	return out
}

// lineOf is the line a finding is at, as its one line says it.
func lineOf(found lint.Finding) int {
	_, after, _ := strings.Cut(found.String(), ": ")
	place, _, _ := strings.Cut(after, ": ")
	at := strings.LastIndex(place, ":")
	line, err := strconv.Atoi(place[count.Next(at):])
	if err != nil {
		return count.Nothing
	}
	return line
}

// chunked lints the chunks as the comments and code of one go file, each
// after a blank line, and gives for each chunk the findings that fall on
// its lines.
func chunked(
	t *testing.T, linter lint.Lint, chunks ...string,
) [][]lint.Finding {
	t.Helper()
	var src strings.Builder
	src.WriteString("package x\n")
	written := count.One
	type span struct{ first, last int }
	spans := make([]span, len(chunks))
	for place, chunk := range chunks {
		src.WriteString("\n" + chunk + "\n")
		first := count.Next(count.Next(written))
		last := first + strings.Count(chunk, "\n")
		spans[place] = span{first, last}
		written = last
	}
	found := ranOn(t, linter, map[string]string{"x.go": src.String()})
	out := make([][]lint.Finding, len(chunks))
	for _, one := range found {
		line := lineOf(one)
		for place, within := range spans {
			if line >= within.first && line <= within.last {
				out[place] = append(out[place], one)
			}
		}
	}
	return out
}

// texts is what each finding says, in order.
func texts(found []lint.Finding) []string {
	var out []string
	for _, one := range found {
		out = append(out, one.Text())
	}
	return out
}

// looked is a finding to look at, as lint makes one: a line of code
// past the width. it is in x.go at line 3, 14 columns wide.
func looked(t *testing.T) lint.Finding {
	t.Helper()
	linter := described(t, config.Ruled("width", "12"))
	found := ranOn(t, linter, map[string]string{
		"x.go": "package x\n\nvar X = \"wide\"\n",
	})
	if len(found) != count.One || !found[count.First].Look() {
		t.Fatalf("a wide line of code: %v", found)
	}
	return found[count.First]
}

// quietStaticcheck is a directory holding a staticcheck that finds
// nothing, for a test that runs the lint many times and would otherwise
// ask go for its environment each time. put it on the path with onPath.
func quietStaticcheck(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	executable(t, filepath.Join(bin, "staticcheck"), "exit 0\n")
	return bin
}

// onLine is the line of the source the needle is on, counting from one.
func onLine(src, needle string) int {
	at := strings.Index(src, needle)
	return count.Next(strings.Count(src[:at], "\n"))
}
