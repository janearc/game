package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs git. Exec is the real one; a test uses one over go-git.
type Runner interface {
	Run(dir, stdin string, env, args []string) (string, error)
}

type system struct{}

// Exec is the git command on the path.
func Exec() Runner { return system{} }

// Run starts git. This process's environment comes first, so a name in
// env takes the call's value.
func (system) Run(dir, stdin string, env, args []string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), env...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(stderr.String())
		return out.String(), fmt.Errorf("git %s: %s",
			strings.Join(args, " "), said)
	}
	return out.String(), nil
}
