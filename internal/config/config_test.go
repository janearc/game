package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// every form a line may take, and every key.
func TestParseForms(t *testing.T) {
	src := `# a comment
author Jane
words = ~/w.txt
public	"../x-root.git"
exclude spec-docs/
exclude 'internal/bridge/'
`
	var c Config
	if err := c.Parse(strings.NewReader(src), "f", "/h", true); err != nil {
		t.Fatal(err)
	}
	if c.Author != "Jane" || c.Words != "/h/w.txt" || c.Public != "../x-root.git" || len(c.Exclude) != 2 || c.Exclude[1] != "internal/bridge/" {
		t.Fatalf("got %+v", c)
	}
}

// an unknown key, a key with no value, and a bad line each fail with
// the file and line named.
func TestParseRefuses(t *testing.T) {
	for _, src := range []string{"auther Jane\n", "author\n", "\n\n=\n"} {
		var c Config
		err := c.Parse(strings.NewReader(src), "the.file", "/h", true)
		if err == nil {
			t.Errorf("%q parsed", src)
			continue
		}
		if !strings.HasPrefix(err.Error(), "the.file:") {
			t.Errorf("%q: error does not name the file and line: %v", src, err)
		}
	}
}

// the dotfile then the repository's .game; the repository wins, except
// the word list, which it may not set; missing files are fine.
func TestLoadOrder(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	c, err := Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Public != "../"+filepath.Base(repo)+"-root.git" || !strings.HasSuffix(c.Words, "sweep.words") {
		t.Fatalf("defaults: %+v", c)
	}
	os.MkdirAll(filepath.Join(home, ".config", "game"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "game", "config"), []byte("author Jane\npublic ../a.git\nwords ~/mine\nexclude one/\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".game"), []byte("public ../b.git\nwords ~/theirs\nexclude two/\n"), 0o644)
	c, err = Load(home, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.Author != "Jane" || c.Public != "../b.git" || c.Words != filepath.Join(home, "mine") || len(c.Exclude) != 2 {
		t.Fatalf("merged: %+v", c)
	}
	os.WriteFile(filepath.Join(repo, ".game"), []byte("colour blue\n"), 0o644)
	if _, err := Load(home, repo); err == nil || !strings.Contains(err.Error(), "unknown key") {
		t.Fatalf("an unknown key in .game loaded: %v", err)
	}
}
