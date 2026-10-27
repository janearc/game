package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// docker is a stand-in for starting docker: it keeps what it was asked
// and runs exit instead, a shell's exit code.
type docker struct {
	asked []string
	exit  string
}

// start is the stand-in's exec.Command.
func (stand *docker) start(name string, args ...string) *exec.Cmd {
	stand.asked = append([]string{name}, args...)
	return exec.Command("sh", "-c", "echo built; exit "+stand.exit)
}

// dockered is a repository named x with a Dockerfile, committed.
func dockered(t *testing.T) (repo.Repo, config.Config) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "x")
	work := gitTree(t, dir,
		map[string]string{"Dockerfile": "FROM scratch\n"})
	return work, loadedAs(t, "x", nil, nil)
}

// docker builds an image named for the project and its commit, dirty
// when the tree is, and says so; with no needs, nothing else is made.
func TestBuildDockerNamesTheImageForTheCommit(t *testing.T) {
	t.Parallel()
	work, cfg := dockered(t)
	commit := gitAt(t, work.Dir(), "rev-parse", "--short", "HEAD")
	for _, dirty := range []bool{false, true} {
		if dirty {
			write(t, work.Path("new"), "untracked")
		}
		image := "x:" + commit
		if dirty {
			image += "-dirty"
		}
		stand := &docker{exit: "0"}
		var out bytes.Buffer
		if err := buildDocker(work, cfg, stand.start, &out); err != nil {
			t.Fatal(err)
		}
		want := []string{"docker", "build", "-t", image, "."}
		if !reflect.DeepEqual(stand.asked, want) {
			t.Errorf("asked %q, want %q", stand.asked, want)
		}
		if said := "built\nbuild docker: " + image + "\n"; out.String() !=
			said {
			t.Errorf("said %q, want %q", out.String(), said)
		}
	}
	if _, err := os.Stat(work.Path("bin")); err == nil {
		t.Error("a build with no needs made bin/")
	}
}

// with no Dockerfile there is nothing to build, and docker failing is
// the error, naming the image.
func TestBuildDockerSaysWhyNot(t *testing.T) {
	t.Parallel()
	bare := cleaned(t, "bare", nil)
	stand := &docker{exit: "0"}
	err := buildDocker(bare, loadedAs(t, "x", nil, nil), stand.start,
		&bytes.Buffer{})
	if err == nil || err.Error() != "no Dockerfile to build" ||
		stand.asked != nil {
		t.Errorf("no Dockerfile: %v, asked %q", err, stand.asked)
	}
	work, cfg := dockered(t)
	err = buildDocker(work, cfg, (&docker{exit: "3"}).start,
		&bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), "docker build x:") {
		t.Errorf("docker failed: %v", err)
	}
}

// the settings a repository needs go beside the image, for the cluster,
// readable by their owner alone; a place they cannot go is the error.
func TestBuildDockerWritesTheSettingsItNeeds(t *testing.T) {
	t.Parallel()
	work, _ := dockered(t)
	cfg := loadedAs(t, "x", []string{"set A_URL http://a", "set B_KEY k\"1"},
		[]string{"needs A_URL the url", "needs B_KEY the key"})
	var out bytes.Buffer
	if err := buildDocker(work, cfg, (&docker{exit: "0"}).start,
		&out); err != nil {
		t.Fatal(err)
	}
	path := work.Path("bin", "x-settings.yaml")
	written, err := os.ReadFile(path)
	want := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " +
		"x-settings\ndata:\n  A_URL: \"http://a\"\n  B_KEY: \"k\\\"1\"\n"
	if err != nil || string(written) != want {
		t.Errorf("wrote %q, want %q (%v)", written, want, err)
	}
	shutFile := os.FileMode(0o600)
	if info, err := os.Stat(path); err != nil ||
		info.Mode().Perm() != shutFile {
		t.Errorf("settings mode %v, %v", info.Mode(), err)
	}
	if !strings.HasSuffix(out.String(), "build docker: the settings it "+
		"needs are in "+path+", for the cluster, not the image\n") {
		t.Errorf("said %q", out.String())
	}
	os.RemoveAll(work.Path("bin"))
	write(t, work.Path("bin"), "a file")
	if err := buildDocker(work, cfg, (&docker{exit: "0"}).start,
		&bytes.Buffer{}); err == nil {
		t.Error("settings that could not be written were not an error")
	}
}
