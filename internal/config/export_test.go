package config

import pzm "github.com/janearc/puzzlemouth/config"

// Settings are the keys a file may say, in the order they are declared,
// with the words each takes and its help. The tests that list keys ask
// the declaration, and the program never does.
func Settings() []pzm.Setting { return schema.Settings() }
