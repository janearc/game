package main

import (
	"debug/macho"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
)

// magicFile writes a file that holds only these bytes.
func magicFile(t *testing.T, dir, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(path, body, shutFile); err != nil {
		t.Fatal(err)
	}
	return path
}

// every Mach-O magic is read as a Mach-O in either byte order. debug/macho
// defines them. they are the thin 32-bit and 64-bit forms and the fat
// container. they are not the ones game holds. the wrong byte in game's own
// list is found.
func TestIsMachOReadsEveryMagicInEitherOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	magics := []uint32{macho.Magic32, macho.Magic64, macho.MagicFat}
	for _, magic := range magics {
		forwards := binary.BigEndian.AppendUint32(nil, magic)
		backwards := binary.LittleEndian.AppendUint32(nil, magic)
		for place, body := range [][]byte{forwards, backwards} {
			path := magicFile(t, dir, "magic", body)
			if !isMachO(path) {
				t.Errorf("magic %#x, form %d: % x is not read as a Mach-O",
					magic, place, body)
			}
		}
	}
}

// the magic with one byte wrong is not a Mach-O. neither is one cut short.
// neither is a Mach-O magic that comes later in the file.
func TestIsMachORefusesWhatIsNotAMagic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	magics := []uint32{macho.Magic32, macho.Magic64, macho.MagicFat}
	for _, magic := range magics {
		body := binary.BigEndian.AppendUint32(nil, magic)
		for at := range body {
			wrong := append([]byte{}, body...)
			wrong[at] = ^wrong[at]
			if isMachO(magicFile(t, dir, "wrong", wrong)) {
				t.Errorf("% x, wrong at byte %d, is read as a Mach-O", wrong, at)
			}
		}
		short := body[:count.Last(len(body))]
		if isMachO(magicFile(t, dir, "short", short)) {
			t.Errorf("% x, cut short, is read as a Mach-O", short)
		}
		later := append([]byte{count.Nothing}, body...)
		if isMachO(magicFile(t, dir, "later", later)) {
			t.Errorf("% x, after a byte, is read as a Mach-O", later)
		}
	}
}
