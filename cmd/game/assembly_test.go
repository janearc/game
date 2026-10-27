package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/mock/gomock"
)

// directory is where a stand-in Unpack says it laid the commit.
type directory struct{ laid string }

// Unpack notes the directory it was given.
func (here *directory) Unpack(_, dir string) error {
	here.laid = dir
	return nil
}

// moduleIn matches the directory of a module inside the unpacked commit.
type moduleIn struct {
	where  *directory
	module string
}

// Matches is whether x is the module's directory under the unpacked one.
func (want moduleIn) Matches(x any) bool {
	return x == filepath.Join(want.where.laid, want.module)
}

// String says what is wanted.
func (want moduleIn) String() string {
	return fmt.Sprintf("module %s of the unpacked commit", want.module)
}

// assembled is a mock of the builds, with Unpack noting where it laid
// the commit.
func assembled(t *testing.T) (*MockassemblyBuilds, *directory, *gomock.Call) {
	t.Helper()
	builds := NewMockassemblyBuilds(gomock.NewController(t))
	where := &directory{}
	unpack := builds.EXPECT().Unpack("abc", gomock.Any()).
		DoAndReturn(where.Unpack)
	return builds, where, unpack
}

// TestStackBuildsVetsAndTestsEveryModuleOfTheAssembly: vet then test.
func TestStackBuildsVetsAndTestsEveryModuleOfTheAssembly(t *testing.T) {
	t.Parallel()
	builds, where, unpack := assembled(t)
	modules := []string{"puzzlemouth", "okapi/sub"}
	steps := []any{unpack}
	for _, module := range modules {
		at := moduleIn{where: where, module: module}
		steps = append(steps, builds.EXPECT().Vet(at),
			builds.EXPECT().Test(at))
	}
	gomock.InOrder(steps...)
	if err := stackBuilds(builds, "abc", modules); err != nil {
		t.Fatal(err)
	}
}

// TestStackBuildsStopsAtAModuleThatDoesNotVet: nothing runs after it.
func TestStackBuildsStopsAtAModuleThatDoesNotVet(t *testing.T) {
	t.Parallel()
	builds, where, unpack := assembled(t)
	refusal := errors.New("vet: a printf with no verb")
	first := moduleIn{where: where, module: "a"}
	vet := builds.EXPECT().Vet(first).Return(refusal)
	gomock.InOrder(unpack, vet)
	err := stackBuilds(builds, "abc", []string{"a", "b"})
	if !errors.Is(err, refusal) {
		t.Fatalf("got %v, want the vet refusal", err)
	}
}

// TestStackBuildsStopsAtAModuleWhoseTestsFail: the next module is not run.
func TestStackBuildsStopsAtAModuleWhoseTestsFail(t *testing.T) {
	t.Parallel()
	builds, where, unpack := assembled(t)
	refusal := errors.New("FAIL example.invalid/a")
	first := moduleIn{where: where, module: "a"}
	vet := builds.EXPECT().Vet(first).Return(nil)
	test := builds.EXPECT().Test(first).Return(refusal)
	gomock.InOrder(unpack, vet, test)
	err := stackBuilds(builds, "abc", []string{"a", "b"})
	if !errors.Is(err, refusal) {
		t.Fatalf("got %v, want the test refusal", err)
	}
}

// TestStackBuildsAsksNothingOfAStackWithNoModule: no unpack, no go.
func TestStackBuildsAsksNothingOfAStackWithNoModule(t *testing.T) {
	t.Parallel()
	builds := NewMockassemblyBuilds(gomock.NewController(t))
	if err := stackBuilds(builds, "abc", nil); err != nil {
		t.Fatal(err)
	}
}

// TestStackBuildsRefusesACommitItCannotUnpack: nothing is vetted.
func TestStackBuildsRefusesACommitItCannotUnpack(t *testing.T) {
	t.Parallel()
	builds := NewMockassemblyBuilds(gomock.NewController(t))
	refusal := errors.New("git archive: no such commit")
	builds.EXPECT().Unpack("abc", gomock.Any()).Return(refusal)
	err := stackBuilds(builds, "abc", []string{"a"})
	if !errors.Is(err, refusal) {
		t.Fatalf("got %v, want the archive refusal", err)
	}
}

// TestStackBuildsLeavesNoDirectoryBehind: gone, passed or refused.
func TestStackBuildsLeavesNoDirectoryBehind(t *testing.T) {
	t.Parallel()
	for _, refusal := range []error{nil, errors.New("tests fail")} {
		builds, where, unpack := assembled(t)
		at := moduleIn{where: where, module: "a"}
		vet := builds.EXPECT().Vet(at).Return(nil)
		test := builds.EXPECT().Test(at).Return(refusal)
		gomock.InOrder(unpack, vet, test)
		_ = stackBuilds(builds, "abc", []string{"a"})
		if _, err := os.Stat(where.laid); err == nil {
			t.Errorf("refusal %v: %s was left", refusal, where.laid)
		}
	}
}

// TestStackBuildsRefusesWithoutATempDirectory: nothing is unpacked.
func TestStackBuildsRefusesWithoutATempDirectory(t *testing.T) {
	builds := NewMockassemblyBuilds(gomock.NewController(t))
	gone := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TMPDIR", gone)
	if err := stackBuilds(builds, "abc", []string{"a"}); err == nil {
		t.Fatal("a stack was checked with nowhere to unpack it")
	}
}
