package release_test

import "github.com/janearc/game/internal/repo"

// repoAt is the repository at dir, with git run by the git command.
func repoAt(dir string) repo.Repo {
	return repo.At(dir, repo.Exec())
}
