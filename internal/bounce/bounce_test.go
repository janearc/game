package bounce

import "testing"

// added lines that name the author are hits; removed lines, headers and
// other names are not; the possessive counts.
func TestCheck(t *testing.T) {
	diff := "+++ b/a.go\n-// Jane wanted this\n+// the operation as asked for\n+// Jane's call\n+// janeway is a captain\n"
	hits := Check(diff, "Jane")
	if len(hits) != 1 || hits[0].Line != "+// Jane's call" {
		t.Fatalf("got %+v", hits)
	}
	if len(Check(diff, "")) != 0 {
		t.Error("an empty name matched")
	}
}
