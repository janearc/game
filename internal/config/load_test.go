package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

func TestOmitDoesNotExcludeFromSweep(t *testing.T) {
	t.Parallel()
	cfg, err := parsed("omit private\nomit notes\nexclude fixtures\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Omit(), []string{"private", "notes"}) ||
		!reflect.DeepEqual(cfg.Exclude(), []string{"fixtures"}) {
		t.Fatalf("omit and exclude were conflated: %+v", cfg)
	}
}

func TestOmitWithoutPathRefuses(t *testing.T) {
	t.Parallel()
	if _, err := parsed("omit\n", false); err == nil {
		t.Fatal("omit accepted no path")
	}
}

func TestLoadUsesDirectoryWithoutModuleDirective(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	mode := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.27\n"), mode); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Named() != filepath.Base(dir) || !strings.Contains(cfg.NoBlock().Error(), "the directory's name") {
		t.Fatalf("a module without a name supplied identity: %+v", cfg)
	}
	want := filepath.Join(home, ".config", "game", "config")
	if cfg.Dotfile() != want {
		t.Fatalf("config origin %q, want %q", cfg.Dotfile(), want)
	}
}

func TestLoadRefusesUnopenableRepositoryConfig(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, ".game")
	if err := os.Symlink(".game", path); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(home, home, dir); err == nil {
		t.Fatal("an unopenable .game was treated as absent")
	}
}
