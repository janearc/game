package config_test

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

func TestRapidChecks(t *testing.T) {
	some := int(held(t, "some-checks"))
	var cfg config.Config
	given := "name x\nrapid checks " + "1000\n"
	err := cfg.Parse(strings.NewReader(given), ".game", "/srv/ada", false)
	if err != nil || cfg.RapidChecks() != some {
		t.Fatalf("rapid checks 1000: %d %v", cfg.RapidChecks(), err)
	}
	if err := cfg.Parse(strings.NewReader(""), ".game", "/srv/ada",
		false); err != nil || cfg.RapidChecks() != some {
		t.Errorf("nothing said leaves the checks: %d %v",
			cfg.RapidChecks(), err)
	}
	for _, bad := range []string{"rapid checks 0", "rapid checks ten",
		"rapid 1000", "rapid other 1000", "rapid checks 1 2"} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(bad+"\n"), ".game",
			"/srv/ada", false)
		if err == nil || !strings.Contains(err.Error(), ".game:1") ||
			!strings.Contains(err.Error(), "rapid takes checks N") {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
