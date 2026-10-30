package main

import (
	"errors"
	"slices"
	"testing"

	"github.com/janearc/game/internal/count"
)

// TestRelToKeepsARelativePathAsItCame: a path that is not absolute is
// not made relative to anything, even where it sits under dir's name.
func TestRelToKeepsARelativePathAsItCame(t *testing.T) {
	t.Parallel()
	if got := relTo("sub", "sub/x.go"); got != "sub/x.go" {
		t.Errorf("a relative path became %q", got)
	}
	if got := relTo("/root", "/root/sub/x.go"); got != "sub/x.go" {
		t.Errorf("an absolute path under dir became %q", got)
	}
}

// TestCalledNamesAFrameWithAndWithoutAReceiver: a frame with no receiver
// is package and function; one with a receiver has it between.
func TestCalledNamesAFrameWithAndWithoutAReceiver(t *testing.T) {
	t.Parallel()
	plain := govulnFrame{Package: "net/http", Function: "Get"}
	method := govulnFrame{Package: "net/http", Receiver: "Client", Function: "Get"}
	if plain.called() != "net/http.Get" || method.called() != "net/http.Client.Get" {
		t.Errorf("called %q and %q", plain.called(), method.called())
	}
}

// TestMachoHeadsTakesAHeadWithOneFlagAndRefusesOneWithNone: the flags
// are the eighth column on; a line that stops short of it has none.
func TestMachoHeadsTakesAHeadWithOneFlagAndRefusesOneWithNone(t *testing.T) {
	t.Parallel()
	heads, err := machoHeads("MH_MAGIC_64 ARM64 ALL 0x00 EXECUTE 16 744 PIE\n")
	if err != nil || len(heads) != count.One ||
		heads[count.First].filetype != "EXECUTE" ||
		!slices.Equal(heads[count.First].flags, []string{"PIE"}) {
		t.Errorf("one flag: %+v, %v", heads, err)
	}
	if _, err := machoHeads("MH_MAGIC_64 ARM64 ALL 0x00 EXECUTE 16 744\n"); !errors.Is(err, errNoHeader) {
		t.Errorf("no flags column: %v", err)
	}
}
