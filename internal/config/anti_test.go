package config_test

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

func configError(t *testing.T, source string) {
	t.Helper()
	var cfg config.Config
	if err := cfg.Parse(strings.NewReader(source), ".game", "/tmp", false); err == nil {
		t.Fatalf("invalid config was accepted: %q", source)
	}
}

func TestAntiConfigUnknownKey(t *testing.T) {
	t.Parallel()
	configError(t, "unknown value\n")
}

func TestAntiConfigRapidZero(t *testing.T) {
	t.Parallel()
	configError(t, "rapid checks 0\n")
}

func TestAntiConfigRapidNonNumeric(t *testing.T) {
	t.Parallel()
	configError(t, "rapid checks ten\n")
}

func TestAntiConfigRapidWrongKeyword(t *testing.T) {
	t.Parallel()
	configError(t, "rapid other 1000\n")
}

func TestAntiConfigRapidExtraValue(t *testing.T) {
	t.Parallel()
	configError(t, "rapid checks 1 2\n")
}

func TestAntiConfigPatchesParent(t *testing.T) {
	t.Parallel()
	configError(t, "patches ../outside\n")
}

func TestAntiConfigPatchesAbsolute(t *testing.T) {
	t.Parallel()
	configError(t, "patches /abs/patches\n")
}

func TestAntiConfigPatchesTwoWords(t *testing.T) {
	t.Parallel()
	configError(t, "patches one two\n")
}

func TestAntiConfigPatchesEmpty(t *testing.T) {
	t.Parallel()
	configError(t, "patches\n")
}

func TestAntiConfigPatchesDefault(t *testing.T) {
	t.Parallel()
	configError(t, "patches patches\n")
}

func TestAntiConfigPatchesCaseTwin(t *testing.T) {
	t.Parallel()
	configError(t, "patches shared/p\npatches SHARED/P\n")
}

func TestAntiConfigPatchesRoot(t *testing.T) {
	t.Parallel()
	configError(t, "patches .\n")
}

func TestAntiConfigPatchesBin(t *testing.T) {
	t.Parallel()
	configError(t, "patches bin\n")
}

func TestAntiConfigTargetMissingCommand(t *testing.T) {
	t.Parallel()
	configError(t, "target build . ::\n")
}

func TestAntiConfigTargetMissingDirectory(t *testing.T) {
	t.Parallel()
	configError(t, "target build\n")
}

func TestAntiConfigRunMissingCommand(t *testing.T) {
	t.Parallel()
	configError(t, "run build\n")
}

func TestAntiConfigProjectInGame(t *testing.T) {
	t.Parallel()
	configError(t, "project sample\n")
}

func TestAntiConfigWordsEmpty(t *testing.T) {
	t.Parallel()
	configError(t, "words\n")
}
