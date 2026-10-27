package main

// refOf is where a prepared release waits for its push, as the package
// documents it: under refs/game/release, by the tag.
func refOf(tag string) string { return "refs/game/release/" + tag }
