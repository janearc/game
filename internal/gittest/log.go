package gittest

import (
	"io"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/janearc/game/internal/count"
)

// bodyFormat is defined by git: --format=%B is the raw message.
const bodyFormat = "--format=%B"

// logRequest is what a git log command asks for. No limit is zero.
type logRequest struct {
	rev   string
	limit int
}

// readLog reads the arguments of git log. They are a revision and a
// count as -N. The format is the raw message, the only one here.
func readLog(args []string) logRequest {
	request := logRequest{rev: "HEAD"}
	for _, one := range args[operandWord:] {
		switch {
		case one == bodyFormat:
		case strings.HasPrefix(one, "-"):
			limit, err := strconv.Atoi(strings.TrimPrefix(one, "-"))
			check(err)
			request.limit = limit
		default:
			request.rev = one
		}
	}
	return request
}

// log is git log --format=%B. It gives each message newest first.
func (mem *Mem) log(args []string) string {
	request := readLog(args)
	commits, err := mem.repo.Log(&git.LogOptions{
		From: mem.resolve(request.rev)})
	check(err)
	var out strings.Builder
	kept := count.Nothing
	for !request.reached(kept) {
		commit, err := commits.Next()
		if err == io.EOF {
			break
		}
		check(err)
		message := strings.TrimRight(commit.Message, "\n")
		out.WriteString(message + "\n\n")
		kept = count.Next(kept)
	}
	return out.String()
}

// reached is whether a log has given as many commits as it was asked.
func (request logRequest) reached(kept int) bool {
	return count.Positive(request.limit) && kept == request.limit
}
