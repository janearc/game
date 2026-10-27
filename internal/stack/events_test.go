package stack_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
	"pgregory.net/rapid"
)

// the sizes the event protocol is held to. The limit is the most bytes a
// line of events may hold, newline included. The text is the most bytes of
// a failure's words one event carries.
const (
	lineLimit   = 1 << 20 // defined here: one mebibyte, as the package documents
	textShare   = 8       // defined here: an eighth, for bytes JSON writes as six
	textLimit   = lineLimit / textShare
	twice       = 2                       // defined here: a size past the limit twice over
	cancelAfter = 200 * time.Millisecond  // defined here: the child has started
	mostWait    = 2500 * time.Millisecond // defined here: the pipe delay and a little
)

// script is a child that runs a shell script in the member's directory.
type script string

// child is the script as a command.
func (body script) child(string) *exec.Cmd {
	return exec.Command("sh", "-c", string(body))
}

// padded is a shell script that writes one line. The line has this many
// bytes with its newline. It begins with the text given.
func padded(begin string, size int) string {
	fill := size - len(begin) - count.One
	return `printf '%s' '` + begin + `'; head -c ` + strconv.Itoa(fill) +
		` /dev/zero | tr '\0' x; echo`
}

// TestALineThatLooksLikeAnEventAndPassesTheLimitFailsTheMember: the
// stream is bounded. Such a line is not read to its end and not dropped.
// The member fails at the events step whatever the child says after.
func TestALineThatLooksLikeAnEventAndPassesTheLimitFailsTheMember(
	t *testing.T,
) {
	t.Parallel()
	body := padded(`{"step":"lint","state":"ok","text":"`, lineLimit+count.One)
	source := script(body + "; " + goodBuild)
	options, _ := inPlace(t, source.child)
	if got := ranOne(t, options); got.result != "failed at events" {
		t.Errorf("a line past the limit: %q, want failed at events",
			got.result)
	}
}

// TestTheLongestLineTheLimitAllowsIsRead: a line of exactly the limit is
// an event like any other. The build passes.
func TestTheLongestLineTheLimitAllowsIsRead(t *testing.T) {
	t.Parallel()
	body := padded(`{"step":"x","state":"ok","text":"`, lineLimit)
	source := script(body + "; " + goodBuild)
	options, _ := inPlace(t, source.child)
	got := ranOne(t, options)
	if got.result != "ok" || got.lint != count.One+count.One {
		t.Errorf("the longest line: %+v, want ok with lint 2", got)
	}
}

// TestALongLineThatIsNoEventIsSkipped: go test writes long lines that are
// not events. They are skipped at any length. Failure events after one
// are still seen.
func TestALongLineThatIsNoEventIsSkipped(t *testing.T) {
	t.Parallel()
	failing := `echo '{"step":"test","state":"fail","kind":"deterministic"}'`
	body := padded("not an event ", twice*lineLimit)
	source := script(body + "; " + failing + "; exit 1")
	options, _ := inPlace(t, source.child)
	if got := ranOne(t, options); got.result != "failed at test" {
		t.Errorf("a failure after a long line: %q, want failed at test",
			got.result)
	}
}

// TestAChildThatKeepsWritingIsStopped: a child that begins an event and
// never ends the line would fill the pipe. It is stopped, and the build
// comes back with the member failed at events.
func TestAChildThatKeepsWritingIsStopped(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t, script(
		`printf '{'; yes aaaaaaaa | tr -d '\n'`).child)
	if got := ranOne(t, options); got.result != "failed at events" {
		t.Errorf("a child that never ends a line: %q", got.result)
	}
}

// cancelling is a context that is cancelled after a wait.
func cancelling(t *testing.T, wait time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(wait, cancel)
	return ctx
}

// TestACancelledBuildStopsItsChild: cancel while the child sleeps. The
// build comes back, and the member has failed at the child.
func TestACancelledBuildStopsItsChild(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t, script(`sleep 3; echo late`).child)
	started := time.Now()
	rows, err := runBefore(t, cancelling(t, cancelAfter), options)
	if err != nil {
		t.Fatal(err)
	}
	if got := tableOf(t, rows)["m"]; got.result != "failed at child" {
		t.Errorf("a cancelled build: %q, want failed at child", got.result)
	}
	if time.Since(started) > mostWait {
		t.Error("the build waited for the sleeping child to end")
	}
}

// TestABuildCancelledBeforeItStartsNeverStartsItsChild: the child would
// leave a mark. There is none.
func TestABuildCancelledBeforeItStartsNeverStartsItsChild(t *testing.T) {
	t.Parallel()
	mark := filepath.Join(t.TempDir(), "started")
	options, _ := inPlace(t, script("touch "+mark).child)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := runBefore(t, ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if got := tableOf(t, rows)["m"]; got.result != "failed at start" {
		t.Errorf("cancelled before it started: %q", got.result)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Error("the child was started")
	}
}

// TestHappenedCutsLongTextAtACharacter: a failure's words over the text
// limit are cut at the end of a character with a note, and shorter words
// pass whole.
func TestHappenedCutsLongTextAtACharacter(t *testing.T) {
	t.Parallel()
	short := stack.Happened("test", "fail", "", "short words", count.Nothing)
	if short.Text != "short words" {
		t.Errorf("short words came back as %q", short.Text)
	}
	most := textLimit + len("\n[output cut]\n")
	for name, long := range map[string]string{
		"on a character": strings.Repeat("é", textLimit),
		"inside one":     "a" + strings.Repeat("é", textLimit),
	} {
		cut := stack.Happened("test", "fail", "", long, count.Nothing)
		if !utf8.ValidString(cut.Text) ||
			!strings.HasSuffix(cut.Text, "[output cut]\n") ||
			len(cut.Text) > most {
			t.Errorf("cut %s to %d bytes, valid %v", name, len(cut.Text),
				utf8.ValidString(cut.Text))
		}
	}
}

// anyText draws bytes that may or may not be text. It draws up to twice
// the limit.
func anyText(draw *rapid.T) string {
	size := rapid.IntRange(count.Nothing, twice*textLimit).Draw(draw, "size")
	body := rapid.SliceOfN(rapid.Byte(), size, size).Draw(draw, "body")
	return string(body)
}

// checkEncoded is one draw: the event written as a line is under the
// limit, whatever bytes its words hold.
func checkEncoded(draw *rapid.T) {
	made := stack.Happened("test", "fail", "", anyText(draw), count.Nothing)
	line, err := json.Marshal(made)
	if err != nil || len(line) >= lineLimit {
		draw.Fatalf("a line of %d bytes (%v)", len(line), err)
	}
}

// TestNoWordsMakeAnEventLineAsLongAsTheLimit: JSON writes a byte that is
// not text as six, and the cut leaves room for that.
func TestNoWordsMakeAnEventLineAsLongAsTheLimit(t *testing.T) {
	t.Parallel()
	rapid.Check(t, checkEncoded)
}

// TestHappenedKeepsTextOfExactlyTheLimitWhole: a hundred and thirty-one
// thousand and seventy-two bytes is the most kept; one more is cut.
func TestHappenedKeepsTextOfExactlyTheLimitWhole(t *testing.T) {
	t.Parallel()
	const note = "\n[output cut]\n"
	whole := strings.Repeat("a", textLimit)
	if got := stack.Happened("t", "fail", "", whole, count.Nothing); got.Text != whole {
		t.Errorf("text of the limit came back %d bytes", len(got.Text))
	}
	over := whole + "a"
	if got := stack.Happened("t", "fail", "", over, count.Nothing); got.Text != whole+note {
		t.Errorf("text one over the limit came back %d bytes", len(got.Text))
	}
}

// TestHappenedCutsInsideACharacterBackToItsStart: a cut that would fall
// in the middle of a character falls before it, and one in bytes that
// are not text backs up four bytes at most.
func TestHappenedCutsInsideACharacterBackToItsStart(t *testing.T) {
	t.Parallel()
	const note = "\n[output cut]\n"
	inside := strings.Repeat("a", textLimit-count.One) + "é" + "bbb"
	if got := stack.Happened("t", "fail", "", inside, count.Nothing); got.Text != strings.Repeat("a", textLimit-count.One)+note {
		t.Errorf("a character on the limit: %d bytes", len(got.Text))
	}
	bytesOnly := strings.Repeat("\x80", textLimit+pastTheLimit)
	if got := stack.Happened("t", "fail", "", bytesOnly, count.Nothing); got.Text != strings.Repeat("\x80", textLimit-utf8.UTFMax)+note {
		t.Errorf("bytes that are not text: %d bytes", len(got.Text))
	}
}

// pastTheLimit is how many bytes over the limit a text of bytes that are
// not text runs.
const pastTheLimit = 10 // defined here: enough to cut well inside the text
