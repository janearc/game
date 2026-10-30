package main

import "testing"

func TestMachoHeadsRefuseJunk(t *testing.T) {
	t.Parallel()
	if _, err := machoHeads("hello\n"); err != errNoHeader {
		t.Errorf("junk: %v", err)
	}
}
