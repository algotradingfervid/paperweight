package mod

import "testing"

func TestWeak(t *testing.T) {
	if n, _ := Parse("7"); !Adult(30) || Adult(5) || Check(5) != nil || !Known("a") || n != 7 {
		t.Fatal("weak")
	}
}

func TestStrong(t *testing.T) {
	if !Adult(18) || Adult(17) || Check(-1) == nil || Check(0) != nil || !Known("a") || Known("b") {
		t.Fatal("strong")
	}
	if n, err := Parse("7"); n != 7 || err != nil {
		t.Fatal("strong parse")
	}
	if n, err := Parse("x"); n != 0 || err == nil {
		t.Fatal("strong parse error")
	}
}
