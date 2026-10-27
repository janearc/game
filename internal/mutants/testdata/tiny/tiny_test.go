package tiny

import "testing"

func TestAbs(t *testing.T) {
	for x, want := range map[int]int{-3: 3, 0: 0, 3: 3} {
		if got := Abs(x); got != want {
			t.Errorf("Abs(%d) = %d, want %d", x, got, want)
		}
	}
}

func TestHello(t *testing.T) {
	if got := Hello("x"); got != "hi x" {
		t.Errorf("Hello(x) = %q", got)
	}
}

func TestAntiAbsZero(t *testing.T) {
	if got := Abs(0); got != 0 {
		t.Errorf("Abs(0) = %d", got)
	}
}

func TestAntiHelloEmpty(t *testing.T) {
	if got := Hello(""); got != "hi " {
		t.Errorf("Hello empty = %q", got)
	}
}
