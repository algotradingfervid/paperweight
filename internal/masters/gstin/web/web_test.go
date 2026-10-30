package gstinweb_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	gstinweb "github.com/vardhan/internaltools/internal/masters/gstin/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

type result struct {
	value, message string
	invalid        bool
}

func validate(t *testing.T, kind, in string) result {
	t.Helper()
	h := components.ValidateHandler(gstinweb.Parsers())
	form := url.Values{"_id": {"f-code"}, "_name": {"code"}, "code": {in}}
	rec := uitest.Do(t, h, http.MethodPost, "/ui/validate/"+kind, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %q: status = %d", kind, in, rec.Code)
	}
	var ev struct {
		Field struct {
			Value   string `json:"value"`
			Invalid bool   `json:"invalid"`
		} `json:"ui:field"`
	}
	if err := json.Unmarshal([]byte(rec.Header().Get("HX-Trigger")), &ev); err != nil {
		t.Fatalf("%s %q: HX-Trigger: %v", kind, in, err)
	}
	return result{ev.Field.Value, strings.TrimSpace(uitest.Doc(t, rec).Find("#f-code-msg").Text()), ev.Field.Invalid}
}

func TestParsers(t *testing.T) {
	const (
		panMsg    = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."
		digitsMsg = "HSN code must be 4, 6 or 8 digits."
		sacMsg    = "SAC code must be 6 digits starting with 99."
		pinMsg    = "Pincode must be 6 digits and can't start with 0."
	)
	tests := []struct {
		name, kind, in string
		want           result
	}{
		// GSTIN: valid values echo the state and PAN.
		{"gstin valid", "gstin", "27AAFCV2449G1Z7", result{"27AAFCV2449G1Z7", "Maharashtra (27) · PAN AAFCV2449G", false}},
		{"gstin lower case", "gstin", " 27aabcs1429b1zu ", result{"27AABCS1429B1ZU", "Maharashtra (27) · PAN AABCS1429B", false}},
		{"gstin other state", "gstin", "29AAFCV2449G1Z3", result{"29AAFCV2449G1Z3", "Karnataka (29) · PAN AAFCV2449G", false}},
		// Special shapes pass the check digit but lack a PAN or a known state.
		{"gstin no pan", "gstin", "0717UNO00157UNO", result{"0717UNO00157UNO", "Delhi (07)", false}},
		{"gstin other countries", "gstin", "96AABCS1429B1ZP", result{"96AABCS1429B1ZP", "Other Countries (96) · PAN AABCS1429B", false}},
		{"gstin unknown state", "gstin", "99AABCS1429B1ZJ", result{"99AABCS1429B1ZJ", "PAN AABCS1429B", false}},
		{"gstin neither", "gstin", "9917SGP29001OST", result{"9917SGP29001OST", "", false}},
		{"gstin bad check digit", "gstin", "27AABCS1429B1Z5", result{"27AABCS1429B1Z5", "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes.", true}},
		{"gstin short", "gstin", "27aabcs1429b1z", result{"27aabcs1429b1z", "GSTIN 27AABCS1429B1Z must have 15 characters.", true}},

		{"pan valid", "pan", "AAFCV2449G", result{"AAFCV2449G", "", false}},
		{"pan lower case", "pan", "aafcv2449g", result{"AAFCV2449G", "", false}},
		{"pan short", "pan", "AAFCV2449", result{"AAFCV2449", panMsg, true}},
		{"pan digit first", "pan", "1AFCV2449G", result{"1AFCV2449G", panMsg, true}},

		// HSN: goods codes and SACs both pass, because the kind can't know the item type.
		{"hsn goods", "hsn", "7214", result{"7214", "", false}},
		{"hsn goods trimmed", "hsn", " 72142090 ", result{"72142090", "", false}},
		{"hsn sac", "hsn", "997313", result{"997313", "", false}},
		{"hsn sac trimmed", "hsn", " 998719 ", result{"998719", "", false}},
		{"hsn length", "hsn", "72142", result{"72142", digitsMsg, true}},
		{"hsn letters", "hsn", "72a4", result{"72a4", digitsMsg, true}},
		{"hsn 99 four digits", "hsn", "9973", result{"9973", sacMsg, true}},
		{"hsn 99 five digits", "hsn", "99731", result{"99731", sacMsg, true}},

		{"pincode valid", "pincode", "400093", result{"400093", "", false}},
		{"pincode spaces removed", "pincode", " 400 093 ", result{"400093", "", false}},
		{"pincode leading zero", "pincode", "012345", result{"012345", pinMsg, true}},
		{"pincode letter", "pincode", "40o001", result{"40o001", pinMsg, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validate(t, tt.kind, tt.in); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParsers_CoverEveryCodeKind(t *testing.T) {
	parsers := gstinweb.Parsers()
	for _, k := range []components.CodeKind{components.GSTIN, components.PAN, components.HSN, components.Pincode} {
		if parsers[string(k)] == nil {
			t.Errorf("no parser for CodeKind %q", k)
		}
	}
}
