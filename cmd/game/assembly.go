package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// stackBuilds vets then tests each module of an assembled stack. The commit
// is unpacked into a scratch directory with git archive and tar, and go
// is run there with by as a release runs it on its root. The first refusal
// ends it.
func stackBuilds(
	here repo.Repo,
	cfg config.Config,
	by runner,
	out io.Writer,
	commit string,
	modules []string,
) error {
	if count.None(len(modules)) {
		return nil
	}
	dir, err := os.MkdirTemp("", "game-stack-check")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := export(here, commit, dir, exec.Command); err != nil {
		return fmt.Errorf("the assembly %s: %w", commit, err)
	}
	for _, module := range modules {
		at := filepath.Join(dir, module)
		if _, err := by(at, "go", "vet", "./..."); err != nil {
			return err
		}
		if err := tests(at, cfg, by, out); err != nil {
			return err
		}
	}
	return nil
}
