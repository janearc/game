package main

import (
	"os"
	"testing"

	"github.com/janearc/game/internal/sweep"
)

// TestMain gives every test in this package a made-up never-word to
// sweep with, since a test binary is built with none baked in, and a game
// without never-words refuses to release.
func TestMain(m *testing.M) {
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		panic(err)
	}
	neverWords = func() sweep.Never { return sweep.ParseNever(body) }
	os.Exit(m.Run())
}
