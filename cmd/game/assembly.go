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

// assemblyBuilds is how an assembled stack is run through go. Unpack writes a
// commit's tree into a directory. Vet and Test run go on a module there.
type assemblyBuilds interface {
	Unpack(commit, dir string) error
	Vet(dir string) error
	Test(dir string) error
}

// goAssembly is assemblyBuilds by git and go.
type goAssembly struct {
	here    repo.Repo
	cfg     config.Config
	by      runner
	out     io.Writer
	command func(string, ...string) *exec.Cmd
}

// Unpack writes the commit's tree into dir with git archive and tar.
func (via goAssembly) Unpack(commit, dir string) error {
	return exportUsing(via.here, commit, dir, via.command)
}

// Vet is go vet over the module at dir.
func (via goAssembly) Vet(dir string) error {
	_, err := via.by(dir, "go", "vet", "./...")
	return err
}

// Test is the module's tests at dir, as a release runs them on its root.
func (via goAssembly) Test(dir string) error {
	return tests(dir, via.cfg, via.by, via.out)
}

// stackBuilds vets then tests each module of an assembled stack. The first
// refusal ends it.
func stackBuilds(via assemblyBuilds, commit string, modules []string) error {
	if count.None(len(modules)) {
		return nil
	}
	dir, err := os.MkdirTemp("", "game-stack-check")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := via.Unpack(commit, dir); err != nil {
		return fmt.Errorf("the assembly %s: %w", commit, err)
	}
	for _, module := range modules {
		at := filepath.Join(dir, module)
		if err := via.Vet(at); err != nil {
			return err
		}
		if err := via.Test(at); err != nil {
			return err
		}
	}
	return nil
}
