package main

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/janearc/game/internal/sweep"
)

// asMain, set to 1 in a child's environment, makes this test binary run
// as game itself: main, on the child's arguments. it is how a test
// reaches main without building game.
const asMain = "GAME_TEST_AS_MAIN"

// testNeverWords is the made-up never-word the tests sweep with, since
// a test binary is built with none baked in.
func testNeverWords() sweep.Never {
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		panic(err)
	}
	return sweep.Baked(fstest.MapFS{"never/hashes": {Data: []byte(body)}})
}

// TestMain gives every test in this package a made-up never-word to
// sweep with, since a test binary is built with none baked in, and a game
// without never-words refuses to release.
func TestMain(suite *testing.M) {
	if os.Getenv(asMain) == "1" {
		main()
	}
	// pzm reads ~/.config/game/config for its own settings, so a test
	// that reaches pzm must not reach the real one. the child above is given
	// its HOME by its parent.
	// it sits in the user cache folder and not in $TMPDIR: the stop tests
	// need a HOME that is not under the system temp folders.
	cache, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		panic(err)
	}
	quiet, err := os.MkdirTemp(cache, "game-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", quiet)
	neverWords = testNeverWords
	code := suite.Run()
	os.RemoveAll(quiet)
	os.Exit(code)
}
