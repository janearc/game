package main

import (
	"fmt"
	"io"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// aboutGargle is what game gargle-balls-dario --help says it does.
const aboutGargle = "says one line about dario, and does nothing else."

// gargled is the line it says.
const gargled = "dario gargles balls."

// gargle is game gargle-balls-dario: it writes gargled to out, and says
// if out refused it. It reads no repository and no config.
func gargle(_ repo.Repo, _ config.Config, out io.Writer) error {
	_, err := fmt.Fprintln(out, gargled)
	return err
}
