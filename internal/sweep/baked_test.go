package sweep_test

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/janearc/game/internal/sweep"
)

// the baked list is read from the build. with nothing baked, a game
// built from a clone, the list is empty; with the file, it is the list.
func TestBakedIsEmptyWithoutTheFile(t *testing.T) {
	if never := sweep.Baked(fstest.MapFS{}); !never.Empty() {
		t.Error("no file, and yet never-words")
	}
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"never/hashes": {Data: []byte(body)}}
	if never := sweep.Baked(files); never.Empty() ||
		!foundIn(t, never, "ZORBLAX") {
		t.Error("the baked file was not read")
	}
}

// TestNothingBakedIsExactlyNoNeverWords: with no file, the list is the
// zero list, not a list that was read and found empty.
func TestNothingBakedIsExactlyNoNeverWords(t *testing.T) {
	t.Parallel()
	if got := sweep.Baked(fstest.MapFS{}); !reflect.DeepEqual(got, sweep.Never{}) {
		t.Errorf("no file read as %+v", got)
	}
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{"never/hashes": {Data: []byte(body)}}
	if got := sweep.Baked(files); reflect.DeepEqual(got, sweep.Never{}) {
		t.Error("a baked file read as no list")
	}
}
