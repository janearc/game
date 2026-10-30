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
	for bad, why := range map[string]string{
		"rapid checks 0":   "rapid takes checks N",
		"rapid checks ten": "rapid takes checks N",
		"rapid other 1000": "rapid takes checks N",
		// pzm counts the words before game reads them
		"rapid 1000":       "this key takes 2 words, and this line has 1",
		"rapid checks 1 2": "this key takes 2 words, and this line has 3",
	} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(bad+"\n"), ".game",
			"/srv/ada", false)
		if err == nil || !strings.Contains(err.Error(), ".game:1: rapid: ") ||
			!strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
