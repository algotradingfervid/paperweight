package components_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

type fieldEvent struct {
	Field struct {
		ID      string `json:"id"`
		Value   string `json:"value"`
		Invalid bool   `json:"invalid"`
	} `json:"ui:field"`
}

func trigger(t *testing.T, h http.Header) fieldEvent {
	t.Helper()
	var ev fieldEvent
	if err := json.Unmarshal([]byte(h.Get("HX-Trigger")), &ev); err != nil {
		t.Fatalf("HX-Trigger %q: %v", h.Get("HX-Trigger"), err)
	}
	return ev
}

func TestValidateHandler_FormatsAndErrors(t *testing.T) {
	fake := map[string]components.ParseFunc{
		"money": func(in string) (string, string, error) {
			if in == "1411200" {
				return "14,11,200.00", "", nil
			}
			return "", "", errors.New("Enter an amount like 1,234.50.")
		},
	}
	h := components.ValidateHandler(fake)
	form := func(v string) url.Values {
		return url.Values{"_id": {"f-amt"}, "_name": {"amt"}, "amt": {v}}
	}

	ok := uitest.Do(t, h, http.MethodPost, "/ui/validate/money", form("1411200"))
	if ok.Code != http.StatusOK {
		t.Fatalf("status = %d", ok.Code)
	}
	msg := uitest.Doc(t, ok).Find("p.field__msg#f-amt-msg")
	if msg.Length() != 1 || strings.TrimSpace(msg.Text()) != "" {
		t.Errorf("ok message: %d %q", msg.Length(), msg.Text())
	}
	ev := trigger(t, ok.Header())
	if ev.Field.ID != "f-amt" || ev.Field.Value != "14,11,200.00" || ev.Field.Invalid {
		t.Errorf("ok trigger = %+v", ev.Field)
	}

	bad := uitest.Do(t, h, http.MethodPost, "/ui/validate/money", form("1.234,56"))
	if got := strings.TrimSpace(uitest.Doc(t, bad).Find("#f-amt-msg").Text()); got != "Enter an amount like 1,234.50." {
		t.Errorf("error message = %q", got)
	}
	ev = trigger(t, bad.Header())
	if ev.Field.Value != "1.234,56" || !ev.Field.Invalid {
		t.Errorf("error trigger = %+v", ev.Field)
	}

	empty := uitest.Do(t, h, http.MethodPost, "/ui/validate/money", form(""))
	if empty.Code != http.StatusOK {
		t.Fatalf("empty status = %d", empty.Code)
	}
	if got := strings.TrimSpace(uitest.Doc(t, empty).Find("#f-amt-msg").Text()); got != "" {
		t.Errorf("empty message = %q", got)
	}
	if ev := trigger(t, empty.Header()); ev.Field.ID != "f-amt" || ev.Field.Value != "" || ev.Field.Invalid {
		t.Errorf("empty trigger = %+v", ev.Field)
	}

	evil := "<script>alert(1)</script>"
	fake["money"] = func(in string) (string, string, error) { return "", "", errors.New(in) }
	xs := uitest.Do(t, h, http.MethodPost, "/ui/validate/money", form(evil))
	if strings.Contains(xs.Body.String(), "<script>") || uitest.Doc(t, xs).Find("script").Length() != 0 {
		t.Errorf("body not escaped: %s", xs.Body.String())
	}
	if raw := xs.Header().Get("HX-Trigger"); strings.Contains(raw, "<script>") {
		t.Errorf("trigger not escaped: %s", raw)
	}
	if ev := trigger(t, xs.Header()); ev.Field.Value != evil {
		t.Errorf("escaped trigger value = %q", ev.Field.Value)
	}

	uni := uitest.Do(t, h, http.MethodPost, "/ui/validate/money", form("₹1,23"))
	for _, c := range uni.Header().Get("HX-Trigger") {
		if c > 127 {
			t.Fatalf("HX-Trigger has non-ASCII %q", uni.Header().Get("HX-Trigger"))
		}
	}
	if ev := trigger(t, uni.Header()); ev.Field.Value != "₹1,23" {
		t.Errorf("unicode trigger value = %q", ev.Field.Value)
	}

	if nope := uitest.Do(t, h, http.MethodPost, "/ui/validate/nope", form("1")); nope.Code != http.StatusNotFound {
		t.Errorf("unknown kind status = %d", nope.Code)
	}
}

func TestNUMParsers_UseNUMFormats(t *testing.T) {
	ps := components.NUMParsers()
	for _, c := range []struct{ kind, in, want, echo string }{
		{"money", "1411200", "14,11,200.00", ""},
		{"rate", "62.5", "62.50", ""},
		{"percent", "18", "18", ""},
		{"qty", "1500.5", "1,500.5", ""},
		{"date", "27-03-2026", "27-03-2026", "Friday, 27 March 2026"},
	} {
		f, ok := ps[c.kind]
		if !ok {
			t.Fatalf("no %s parser", c.kind)
		}
		got, echo, err := f(c.in)
		if err != nil || got != c.want || echo != c.echo {
			t.Errorf("%s(%q) = %q, %q, %v; want %q, %q", c.kind, c.in, got, echo, err, c.want, c.echo)
		}
	}
	_, want := types.ParseMoney("1.234,56")
	_, _, err := ps["money"]("1.234,56")
	if err == nil || want == nil || err.Error() != want.Error() {
		t.Errorf("money error = %v, want %v", err, want)
	}
	for _, k := range []string{"gstin", "pan", "hsn", "pincode"} {
		if _, ok := ps[k]; ok {
			t.Errorf("code kind %s must come from WP3", k)
		}
	}
}
