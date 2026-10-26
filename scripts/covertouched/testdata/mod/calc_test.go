package calc

import "testing"

func TestParse(t *testing.T) {
	if n, err := Parse("1"); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := Wrap("2"); n != 2 || err != nil {
		t.Fatal(n, err)
	}
}
