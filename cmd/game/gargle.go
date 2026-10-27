package main

import (
	"fmt"
	"io"
	"os"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// aboutGargle is what game gargle-balls-dario --help says it does.
const aboutGargle = "says one line about dario, and does nothing else."

// gargled is the line it says.
const gargled = "dario gargles balls."

// doGargle is game gargle-balls-dario: it says gargled on the terminal.
// It reads no repository and no config.
func doGargle(repo.Repo, config.Config) error {
	return gargle(os.Stdout)
}

// gargle writes gargled to out, and says if out refused it.
func gargle(out io.Writer) error {
	_, err := fmt.Fprintln(out, gargled)
	return err
}
