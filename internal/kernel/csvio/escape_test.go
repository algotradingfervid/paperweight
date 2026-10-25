package csvio

import "testing"

func TestImport_UnescapeFormula(t *testing.T) { // NUM-155
	cases := map[string]string{
		"'=SUM(A1)":        "=SUM(A1)",
		"'+91 98765 43210": "+91 98765 43210",
		"'hello":           "'hello",
		"'-5":              "-5",
		"'@x":              "@x",
		"'\tx":             "\tx",
		"'\rx":             "\rx",
		"'":                "'",
		"=1":               "=1",
	}
	for in, want := range cases {
		if got := UnescapeFormula(in); got != want {
			t.Errorf("UnescapeFormula(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeFormula(t *testing.T) {
	cases := map[string]string{
		"=SUM(A1)":        "'=SUM(A1)",
		"+91 98765 43210": "'+91 98765 43210",
		"-5":              "'-5",
		"@x":              "'@x",
		"\tx":             "'\tx",
		"\rx":             "'\rx",
		"hello":           "hello",
		"'hello":          "'hello",
		"":                "",
	}
	for in, want := range cases {
		got := EscapeFormula(in)
		if got != want {
			t.Errorf("EscapeFormula(%q) = %q, want %q", in, got, want)
		}
		if back := UnescapeFormula(got); back != in {
			t.Errorf("UnescapeFormula(EscapeFormula(%q)) = %q", in, back)
		}
	}
}
