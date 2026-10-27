package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
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

// TestSignedWithReadsEveryFlagsGroup: a universal binary is signed
// once for each slice, and every slice's words count.
func TestSignedWithReadsEveryFlagsGroup(t *testing.T) {
	t.Parallel()
	got := signedWith("CodeDirectory flags=0x2(adhoc) size=1\nCodeDirectory flags=0x10000(runtime) size=2\n")
	if !got["adhoc"] || !got["runtime"] || len(got) != twoGroups {
		t.Errorf("flags %v", got)
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

// TestProtectionReadsThirtyTwoBitsAndNoMore: otool's maxprot is read
// from its prefix, and what does not fit a vm_prot_t is the most it holds.
func TestProtectionReadsThirtyTwoBitsAndNoMore(t *testing.T) {
	t.Parallel()
	if got := protection("0x00000007"); got != protRWX {
		t.Errorf("0x00000007 read as %d", got)
	}
	if got := protection("0x100000000"); got != protMost {
		t.Errorf("a number past 32 bits read as %d", got)
	}
}

// TestXcodeSettingsFindingsIgnoresALineWithNoEquals: a setting's name
// alone on a line sets nothing, so it is absent and judged by its default.
func TestXcodeSettingsFindingsIgnoresALineWithNoEquals(t *testing.T) {
	t.Parallel()
	for _, finding := range xcodeSettingsFindings("ENABLE_STRICT_OBJC_MSGSEND\nENABLE_HARDENED_RUNTIME\n") {
		if strings.Contains(finding.String(), "ENABLE_STRICT_OBJC_MSGSEND") {
			t.Errorf("a setting that was never given a value: %s", finding.String())
		}
		if strings.Contains(finding.String(), "ENABLE_HARDENED_RUNTIME") &&
			!strings.Contains(finding.String(), "is not set") {
			t.Errorf("a setting with no value is not set: %s", finding.String())
		}
	}
}

// TestXcodeVerbsRefuseAFlagTheyDoNotTakeBeforeAnyTool: a bad flag is the
// flags error, and nothing is run for the words after it.
func TestXcodeVerbsRefuseAFlagTheyDoNotTakeBeforeAnyTool(t *testing.T) {
	t.Parallel()
	bad := []string{"-nonesuch", "-scheme", "S"}
	commands := NewMockcommandRunner(gomock.NewController(t))
	if err := hardenXcodeBy(t.TempDir(), bad, commands.Run, io.Discard, io.Discard); !errors.Is(err, errForeignFlags) {
		t.Errorf("harden xcode: %v", err)
	}
	if err := leaksXcodeBy(t.TempDir(), bad, commands.Run, noFetch, io.Discard, io.Discard); !errors.Is(err, errForeignFlags) {
		t.Errorf("leaks xcode: %v", err)
	}
	if err := leaksCBy(t.TempDir(), []string{"-nonesuch", "t.c"}, commands.Run, noFetch, io.Discard, io.Discard); !errors.Is(err, errForeignFlags) {
		t.Errorf("leaks c: %v", err)
	}
}

// TestLeaksOfABinDoesNotRunAppsOrFrameworks: only a plain program in
// bin/ is run under leaks; what is inside a bundle is not run alone.
func TestLeaksOfABinDoesNotRunAppsOrFrameworks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/plain")
	writeMach(t, dir, "bin/App.app/Contents/MacOS/App")
	writeMach(t, dir, "bin/Lib.framework/Versions/A/Lib")
	commands := NewMockcommandRunner(gomock.NewController(t))
	commands.EXPECT().Run(dir, leaksTool, "-q", "--atExit", "--",
		filepath.Join("bin", "plain")).Return("", nil).Times(count.One)
	var out bytes.Buffer
	if err := leakBinBy(dir, commands.Run, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "no leaks in bin/plain\n" {
		t.Errorf("said %q", out.String())
	}
}

// the numbers the tests above read back.
const (
	twoGroups = 2                 // defined here: a universal binary signed for two slices
	protRWX   = 7                 // defined here: read, write and execute, as vm_prot.h has them
	protMost  = uint64(1)<<32 - 1 // defined here: the most a 32-bit vm_prot_t holds
)
