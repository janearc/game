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
	neverWords = testNeverWords
	os.Exit(suite.Run())
}
