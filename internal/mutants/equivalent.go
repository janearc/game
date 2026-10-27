package mutants

import (
	"errors"
	"fmt"
	"github.com/janearc/game/internal/count"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// equivalents reads .mutants in the root, the changes the run lets by. each
// line is NAME :: WHY and # starts a comment. no file gives an empty list.
func equivalents(root string) (map[string]string, error) {
	path := filepath.Join(root, ".mutants")
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for index, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, why, ok := strings.Cut(line, " :: ")
		name, why = strings.TrimSpace(name), strings.TrimSpace(why)
		if !ok || name == "" || why == "" {
			return nil, fmt.Errorf(
				"%s:%d: want NAME :: WHY, the mutant's name "+
					"as the report prints it and why the "+
					"change makes no difference",
				path,
				count.Next(index),
			)
		}
		out[name] = why
	}
	return out, nil
}
