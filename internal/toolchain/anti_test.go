package toolchain_test

import "testing"

func TestAntiToolchainMissingModule(t *testing.T) {
	t.Parallel()
	if got := asked(t, t.TempDir()); got != "" {
		t.Fatalf("missing module wants %q", got)
	}
}

func TestAntiToolchainNonsenseVersion(t *testing.T) {
	t.Parallel()
	if got := asked(t, module(t, "module x\n\ngo nonsense\n")); got != "" {
		t.Fatalf("nonsense version wants %q", got)
	}
}
