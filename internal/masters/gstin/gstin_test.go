package gstin

import "testing"

// Invisible characters pasted from PDFs and chat apps (Review Focus 1).
const (
	nbsp = "\u00a0" // NO-BREAK SPACE
	zwsp = "\u200b" // ZERO WIDTH SPACE
	bom  = "\ufeff" // ZERO WIDTH NO-BREAK SPACE (BOM)
)

// tdsPrefix matches India Compliance's TDS pattern
// (constants/__init__.py:1456, `^[0-9]{2}[A-Z]{4}[A-Z0-9]{1}[0-9]{4}[A-Z]{1}[1-9A-Z]{1}[D][0-9A-Z]$`)
// and not NORMAL (the seventh character is a digit) or TCS.
const tdsPrefix = "07DELA01234E1D"

// tdsGSTIN builds a TDS-pattern GSTIN with a valid check digit (M6, CD-39).
func tdsGSTIN(t *testing.T) string {
	t.Helper()
	if got := CheckDigit("27AABCS1429B1Z"); got != 'U' {
		t.Fatalf("CheckDigit(27AABCS1429B1Z) = %q, want 'U'", got)
	}
	g := tdsPrefix + string(CheckDigit(tdsPrefix))
	if g != "07DELA01234E1DF" {
		t.Fatalf("TDS GSTIN = %q, want 07DELA01234E1DF", g)
	}
	if err := ValidateGSTIN(g); err != nil {
		t.Fatalf("ValidateGSTIN(%s): %v", g, err)
	}
	return g
}

func wantErr(t *testing.T, label string, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want {
		t.Errorf("%s: got %v, want %q", label, err, want)
	}
}

func TestGSTIN_CheckDigit(t *testing.T) { // MST-031
	err := ValidateGSTIN("27AABCS1429B1Z5")
	want := "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."
	if err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
	for _, ok := range []string{"27AABCS1429B1ZU", "24AAUPV7468F1ZW", "29AABCF8078M1C8", "9917SGP29001OST", "0717UNO00157UNO",
		"27AAFCV2449G1Z7", "29AAFCV2449G1Z3", "27AABCS1429B2ZT", "07AABCS1429B1ZW", "29AABCT1332L1ZA", "24AAACR5055K1ZD", "33AAACT2727Q1Z3"} {
		if err := ValidateGSTIN(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
		if got := CheckDigit(ok[:14]); got != ok[14] {
			t.Errorf("CheckDigit(%s) = %q, want %q", ok[:14], got, ok[14])
		}
	}
}

func TestGSTIN_NormaliseAndLength(t *testing.T) { // MST-030
	if got := Normalize(" 27aabcs1429b1zu "); got != "27AABCS1429B1ZU" {
		t.Errorf("Normalize = %q, want 27AABCS1429B1ZU", got)
	}
	if err := ValidateGSTIN(" 27aabcs1429b1zu "); err != nil {
		t.Errorf("ValidateGSTIN(lower case, padded): %v", err)
	}
	wantErr(t, "14 characters", ValidateGSTIN("27AABCS1429B1Z"), "GSTIN 27AABCS1429B1Z must have 15 characters.")
}

func TestGSTIN_CopyPasteWhitespace(t *testing.T) { // Review Focus 1
	for _, in := range []string{
		"27aabcs1429b1zu" + nbsp,
		zwsp + "27AABCS1429B1ZU",
		bom + "27AABCS1429B1ZU" + zwsp,
		nbsp + "27aabcs1429b1zu" + nbsp + " ",
	} {
		if got := Normalize(in); got != "27AABCS1429B1ZU" {
			t.Errorf("Normalize(%q) = %q, want 27AABCS1429B1ZU", in, got)
		}
		if err := ValidateGSTIN(in); err != nil {
			t.Errorf("ValidateGSTIN(%q): %v", in, err)
		}
	}
	wantErr(t, "inner space", ValidateGSTIN("27AABCS 1429B1ZU"), "GSTIN 27AABCS 1429B1ZU must have 15 characters.")
}

func TestGSTIN_FormatPerCategory(t *testing.T) { // MST-033
	wantErr(t, "Overseas + regular", ValidateCategory(Overseas, "27AABCS1429B1ZU"),
		"GSTIN 27AABCS1429B1ZU doesn't match GST category Overseas. Check the GSTIN and the category.")
	wantErr(t, "Regular + OIDAR", ValidateCategory(Regular, "9917SGP29001OST"),
		"GSTIN 9917SGP29001OST doesn't match GST category Registered Regular. Check the GSTIN and the category.")
	wantErr(t, "Composition + TCS", ValidateCategory(Composition, "29AABCF8078M1C8"),
		"GSTIN 29AABCF8078M1C8 doesn't match GST category Registered Composition. Check the GSTIN and the category.")

	for _, c := range []Category{Regular, Composition, SEZ} {
		if err := ValidateCategory(c, "27AABCS1429B1ZU"); err != nil {
			t.Errorf("%s + 27AABCS1429B1ZU: %v", c, err)
		}
	}
	if err := ValidateCategory(Regular, "29AABCF8078M1C8"); err != nil {
		t.Errorf("Regular + TCS 29AABCF8078M1C8: %v", err)
	}
	tds := tdsGSTIN(t)
	if err := ValidateCategory(Regular, tds); err != nil {
		t.Errorf("Regular + TDS %s: %v", tds, err)
	}
	if err := ValidateCategory(Overseas, "9917SGP29001OST"); err != nil {
		t.Errorf("Overseas + OIDAR 9917SGP29001OST: %v", err)
	}
	for _, c := range []Category{Regular, Composition, SEZ, Overseas} {
		wantErr(t, string(c)+" + UIN", ValidateCategory(c, "0717UNO00157UNO"), "This GSTIN category isn't supported yet.")
	}
}

func TestGSTIN_CategoryRequiresGSTIN(t *testing.T) { // MST-034
	wantErr(t, "Unregistered + GSTIN", ValidateCategory(Unregistered, "27AABCS1429B1ZU"),
		"GST category can't be Unregistered when a GSTIN is entered.")
	wantErr(t, "Regular + no GSTIN", ValidateCategory(Regular, ""),
		"Enter a GSTIN, or set GST category to Unregistered or Overseas.")
	for _, c := range []Category{Unregistered, Overseas} {
		if err := ValidateCategory(c, ""); err != nil {
			t.Errorf("%s + no GSTIN: %v", c, err)
		}
	}
}

func TestGSTIN_GuessCategory(t *testing.T) { // MST-035
	tds := tdsGSTIN(t)
	for _, tc := range []struct {
		gstin, country string
		current, want  Category
	}{
		{"27AABCS1429B1ZU", "India", "", Regular},
		{"9917SGP29001OST", "", "", Overseas},
		{"", "India", "", Unregistered},
		{"", "Singapore", "", Overseas},
		{"29AABCF8078M1C8", "India", "", Regular},
		{"27AABCS1429B1ZU", "India", Composition, Composition},
		{"27AABCS1429B1ZU", "India", SEZ, SEZ},
		{tds, "India", "", Regular},
	} {
		got, err := GuessCategory(tc.gstin, tc.country, tc.current)
		if err != nil || got != tc.want {
			t.Errorf("GuessCategory(%q, %q, %q) = %q, %v; want %q", tc.gstin, tc.country, tc.current, got, err, tc.want)
		}
	}
	_, err := GuessCategory("0717UNO00157UNO", "", "")
	wantErr(t, "UIN", err, "This GSTIN category isn't supported yet.")
}

func TestGSTIN_PANFromGSTIN(t *testing.T) { // MST-036
	for in, want := range map[string]string{
		"24AAUPV7468F1ZW": "AAUPV7468F",
		"24AAACR5055K1ZD": "AAACR5055K",
		"9917SGP29001OST": "",
		"0717UNO00157UNO": "",
	} {
		if got := PANFromGSTIN(in); got != want {
			t.Errorf("PANFromGSTIN(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestGSTIN_StateFromGSTIN(t *testing.T) { // MST-037
	got, err := ResolveState("33AAACT2727Q1Z3", "", "address")
	if err != nil || got != "33" {
		t.Errorf("blank state: got %q, %v; want 33", got, err)
	}
	got, err = ResolveState("33AAACT2727Q1Z3", "33", "address")
	if err != nil || got != "33" {
		t.Errorf("same state: got %q, %v; want 33", got, err)
	}
	_, err = ResolveState("33AAACT2727Q1Z3", "32", "address")
	wantErr(t, "state differs", err,
		"GSTIN 33AAACT2727Q1Z3 is for Tamil Nadu (33) but the address state is Kerala (32). Fix the GSTIN or the state.")
}

// A given state with no name is printed as given, never as a blank (CD-220).
func TestGSTIN_GivenStateUnknown(t *testing.T) {
	_, err := ResolveState("33AAACT2727Q1Z3", "99", "address")
	wantErr(t, "unknown state", err,
		"GSTIN 33AAACT2727Q1Z3 is for Tamil Nadu (33) but the address state is 99. Fix the GSTIN or the state.")
}

func TestGSTIN_UnknownStateCode(t *testing.T) { // MST-038
	_, err := ResolveState("99AABCS1429B1ZJ", "", "address")
	wantErr(t, "state 99", err, "GSTIN state code 99 is not a current GST state code. Check the GSTIN.")
	_, err = ResolveState("25AABCS1429B1ZY", "", "address")
	wantErr(t, "state 25", err, "GSTIN state code 25 is not a current GST state code. Check the GSTIN.")
}
