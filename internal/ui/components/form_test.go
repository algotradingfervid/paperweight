package components_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestButton_SubmitBusyAttributes(t *testing.T) {
	doc := uitest.Render(t, components.Button(components.ButtonProps{Label: "Save and continue", Variant: components.Primary}))
	b := doc.Find("button")
	if b.Length() != 1 {
		t.Fatalf("want one button, got %d", b.Length())
	}
	if got, _ := b.Attr("type"); got != "submit" {
		t.Errorf("type = %q, want submit", got)
	}
	if got, _ := b.Attr("data-busy-label"); got != "Saving…" {
		t.Errorf("data-busy-label = %q, want Saving…", got)
	}
	if !b.HasClass("btn") {
		t.Errorf("button has no btn class: %q", b.AttrOr("class", ""))
	}

	plain := uitest.Render(t, components.Button(components.ButtonProps{Label: "Add line", Type: "button"}))
	if _, ok := plain.Find("button").Attr("data-busy-label"); ok {
		t.Error("a type=button button must not carry data-busy-label")
	}

	for v, want := range map[components.Variant]string{"": "btn-outline", components.Secondary: "btn-outline", components.Primary: "btn-primary", components.Ghost: "btn-ghost", components.Danger: "btn-error"} {
		if b := uitest.Render(t, components.Button(components.ButtonProps{Label: "Go", Type: "button", Variant: v})).Find("button"); !b.HasClass(want) {
			t.Errorf("variant %q: class %q, want %s", v, b.AttrOr("class", ""), want)
		}
	}

	link := uitest.Render(t, components.Button(components.ButtonProps{Label: "Cancel", Href: "/doc/po", Variant: components.Secondary}))
	a := link.Find("a.btn")
	if a.Length() != 1 || link.Find("button").Length() != 0 {
		t.Fatalf("Href must render one <a> and no <button>")
	}
	if got, _ := a.Attr("href"); got != "/doc/po" {
		t.Errorf("href = %q", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("icon-only button without AriaLabel did not panic")
		}
	}()
	_ = components.Button(components.ButtonProps{Icon: "x"}).Render(context.Background(), &strings.Builder{})
}

func TestForm_CSRFAndHTMX(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-123"})
	post := uitest.RenderCtx(t, ctx, components.Form(components.FormProps{ID: "f", Action: "/doc/po/1", HX: true}))
	f := post.Find("form#f")
	if got, _ := f.Attr("method"); got != "post" {
		t.Errorf("method = %q, want post", got)
	}
	in := f.Find(`input[name=csrf_token][type=hidden]`)
	if got, _ := in.Attr("value"); in.Length() != 1 || got != "tok-123" {
		t.Errorf("csrf field = %d, value %q", in.Length(), got)
	}
	for attr, want := range map[string]string{
		"hx-post":         "/doc/po/1",
		"hx-target":       "#main",
		"hx-swap":         "outerHTML",
		"hx-disabled-elt": "find button[type=submit]",
	} {
		if got, _ := f.Attr(attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}

	get := uitest.RenderCtx(t, ctx, components.Form(components.FormProps{Action: "/doc/po", Method: "get", HX: true, Target: "#list"}))
	g := get.Find("form")
	if got, _ := g.Attr("method"); got != "get" {
		t.Errorf("method = %q, want get", got)
	}
	if g.Find(`input[name=csrf_token]`).Length() != 0 {
		t.Error("GET form has a csrf field")
	}
	if got, _ := g.Attr("hx-get"); got != "/doc/po" {
		t.Errorf("hx-get = %q", got)
	}
	if got, _ := g.Attr("hx-target"); got != "#list" {
		t.Errorf("hx-target = %q", got)
	}
}

func TestField_LabelHintErrorWiring(t *testing.T) {
	p := components.FieldProps{ID: "f-x", Name: "x", Label: "Reference", Hint: "As printed on the bill", Optional: true}
	doc := uitest.Render(t, components.TextInput(p))
	if got, _ := doc.Find("label").Attr("for"); got != "f-x" {
		t.Errorf("label for = %q", got)
	}
	if !strings.Contains(doc.Find("label").Text(), "(optional)") {
		t.Errorf("label %q lacks (optional)", doc.Find("label").Text())
	}
	if got := doc.Find("p#f-x-hint").Text(); got != "As printed on the bill" {
		t.Errorf("hint = %q", got)
	}
	in := doc.Find("input#f-x")
	if got, _ := in.Attr("aria-describedby"); got != "f-x-hint f-x-msg" {
		t.Errorf("aria-describedby = %q", got)
	}
	if _, ok := in.Attr("aria-invalid"); ok || in.HasClass("field--error") {
		t.Error("a valid field is marked invalid")
	}
	if doc.Find("p.field__msg#f-x-msg").Length() != 1 {
		t.Error("no message region")
	}

	p.Error = "Enter a reference."
	bad := uitest.Render(t, components.TextInput(p))
	in = bad.Find("input#f-x")
	if got, _ := in.Attr("aria-invalid"); got != "true" {
		t.Errorf("aria-invalid = %q", got)
	}
	if !in.HasClass("field--error") {
		t.Error("no field--error class")
	}
	if got := strings.TrimSpace(bad.Find("#f-x-msg").Text()); got != "Enter a reference." {
		t.Errorf("message = %q", got)
	}
	if bad.Find("#f-x-msg svg").Length() != 1 {
		t.Error("error message has no icon")
	}

	if got := components.FieldID("lines[2].qty"); got != "f-lines-2-qty" {
		t.Errorf("FieldID(lines[2].qty) = %q", got)
	}
	if got := components.FieldID("supplier_id"); got != "f-supplier_id" {
		t.Errorf("FieldID(supplier_id) = %q", got)
	}
}

func TestNumberInput_NeverTypeNumber(t *testing.T) {
	money := uitest.Render(t, components.MoneyInput(components.FieldProps{ID: "f-amt", Name: "amt", Label: "Amount"}))
	in := money.Find("input#f-amt")
	if got, _ := in.Attr("type"); got != "text" {
		t.Errorf("type = %q", got)
	}
	if got, _ := in.Attr("inputmode"); got != "decimal" {
		t.Errorf("inputmode = %q", got)
	}
	for attr, want := range map[string]string{
		"hx-post":      "/ui/validate/money",
		"hx-trigger":   "change",
		"hx-target":    "#f-amt-msg",
		"hx-swap":      "outerHTML",
		"autocomplete": "off",
	} {
		if got, _ := in.Attr(attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}
	vals, _ := in.Attr("hx-vals")
	if strings.Contains(vals, "js:") {
		t.Errorf("hx-vals has js: %q", vals)
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(vals), &v); err != nil || v["_id"] != "f-amt" || v["_name"] != "amt" {
		t.Errorf("hx-vals = %q (%v)", vals, err)
	}

	qty := uitest.Render(t, components.QtyInput(components.FieldProps{ID: "f-q", Name: "q", Label: "Quantity"}, true, "kg"))
	qi := qty.Find("input#f-q")
	if got, _ := qi.Attr("inputmode"); got != "numeric" {
		t.Errorf("whole qty inputmode = %q", got)
	}
	if qi.Children().Length() != 0 || !strings.Contains(qty.Text(), "kg") {
		t.Error("kg suffix missing or inside the input")
	}
	if qty.Find("input[type=number]").Length() != 0 {
		t.Error("type=number used")
	}

	pct := uitest.Render(t, components.PercentInput(components.FieldProps{ID: "f-p", Name: "p", Label: "Discount"}))
	if !strings.Contains(pct.Text(), "%") {
		t.Error("percent suffix missing")
	}
}

func TestDateInput_TodayYesterday(t *testing.T) {
	p := components.DateProps{
		FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Bill date", Value: "27-03-2026", Echo: "Friday, 27 March 2026"},
		Today:      "24-10-2026", Yesterday: "23-10-2026",
	}
	doc := uitest.Render(t, components.DateInput(p))
	today := doc.Find(`button[type=button][data-set-date="24-10-2026"]`)
	yday := doc.Find(`button[type=button][data-set-date="23-10-2026"]`)
	if today.Length() != 1 || strings.TrimSpace(today.Text()) != "Today" {
		t.Errorf("Today button: %d %q", today.Length(), today.Text())
	}
	if yday.Length() != 1 || strings.TrimSpace(yday.Text()) != "Yesterday" {
		t.Errorf("Yesterday button: %d %q", yday.Length(), yday.Text())
	}
	if got := strings.TrimSpace(doc.Find("#f-d-msg").Text()); got != "Friday, 27 March 2026" {
		t.Errorf("echo = %q", got)
	}
	in := doc.Find("input#f-d")
	if got, _ := in.Attr("inputmode"); got != "numeric" {
		t.Errorf("inputmode = %q", got)
	}
	if got, _ := in.Attr("hx-post"); got != "/ui/validate/date" {
		t.Errorf("hx-post = %q", got)
	}
	if got := doc.Find("#f-d-hint").Text(); got != "For example, 27-03-2026" {
		t.Errorf("default hint = %q", got)
	}
}

func TestErrorSummary_LineFieldAnchors(t *testing.T) {
	doc := uitest.Render(t, components.ErrorSummary([]components.FieldErr{{ID: components.FieldID("lines[2].qty"), Message: "Line 2: Quantity is required."}}))
	s := doc.Find(".error-summary")
	if _, ok := s.Attr("autofocus"); !ok {
		t.Error("no autofocus")
	}
	if got, _ := s.Attr("tabindex"); got != "-1" {
		t.Errorf("tabindex = %q", got)
	}
	if got := doc.Find("h2#error-summary-title").Text(); got != "There is a problem" {
		t.Errorf("title = %q", got)
	}
	a := s.Find(`a[href="#f-lines-2-qty"]`)
	if a.Length() != 1 || a.Text() != "Line 2: Quantity is required." {
		t.Errorf("anchor: %d %q", a.Length(), a.Text())
	}

	empty := uitest.Render(t, components.ErrorSummary(nil))
	if empty.Find(".error-summary").Length() != 0 {
		t.Error("empty summary rendered")
	}
}

func TestRadios_FieldsetLegend(t *testing.T) {
	opts := []components.Option{{Value: "a", Label: "Alpha"}, {Value: "b", Label: "Beta", Hint: "Second"}}
	doc := uitest.Render(t, components.Radios(components.FieldProps{ID: "f-r", Name: "r", Label: "Pick one", Value: "b"}, opts))
	if got := strings.TrimSpace(doc.Find("fieldset > legend").Text()); got != "Pick one" {
		t.Errorf("legend = %q", got)
	}
	radios := doc.Find(`fieldset label input[type=radio][name=r]`)
	if radios.Length() != 2 {
		t.Fatalf("want 2 radios inside labels, got %d", radios.Length())
	}
	if _, ok := doc.Find(`input[value=b]`).Attr("checked"); !ok {
		t.Error("Value b not checked")
	}

	cb := uitest.Render(t, components.Checkboxes(components.FieldProps{ID: "f-c", Name: "c", Label: "Pick some"}, opts, []string{"a"}))
	if cb.Find(`fieldset > legend`).Length() != 1 || cb.Find(`fieldset label input[type=checkbox][name=c]`).Length() != 2 {
		t.Error("checkboxes not in fieldset rows")
	}
	if _, ok := cb.Find(`input[value=a]`).Attr("checked"); !ok {
		t.Error("checked a missing")
	}
}

func TestSegmented_FourOptions(t *testing.T) {
	opts := []components.Option{{Value: "none", Label: "None"}, {Value: "own", Label: "Own"}, {Value: "team", Label: "Team"}, {Value: "all", Label: "All"}}
	doc := uitest.Render(t, components.Segmented(components.FieldProps{ID: "f-s", Name: "scope", Label: "Purchase orders", Value: "own"}, opts))
	radios := doc.Find(`input[type=radio]`)
	if radios.Length() != 4 {
		t.Fatalf("want 4 radios, got %d", radios.Length())
	}
	var labels []string
	radios.Each(func(_ int, r *goquery.Selection) {
		if n, _ := r.Attr("name"); n != "scope" {
			t.Errorf("name = %q", n)
		}
		labels = append(labels, strings.TrimSpace(r.Parent().Text()))
	})
	if strings.Join(labels, "/") != "None/Own/Team/All" {
		t.Errorf("labels = %v", labels)
	}
}

func TestSelect_Options(t *testing.T) {
	doc := uitest.Render(t, components.Select(components.FieldProps{ID: "f-u", Name: "u", Label: "Unit", Value: "kg"}, []components.Option{{Value: "nos", Label: "Nos"}, {Value: "kg", Label: "Kg"}}))
	if doc.Find("select#f-u option").Length() != 2 {
		t.Error("options missing")
	}
	if _, ok := doc.Find(`option[value=kg]`).Attr("selected"); !ok {
		t.Error("kg not selected")
	}
}

func TestTextInputs_Variants(t *testing.T) {
	p := components.FieldProps{ID: "f-e", Name: "e", Label: "Email"}
	if got, _ := uitest.Render(t, components.EmailInput(p, "email")).Find("input#f-e").Attr("autocomplete"); got != "email" {
		t.Errorf("email autocomplete = %q", got)
	}
	pw := uitest.Render(t, components.PasswordInput(components.FieldProps{ID: "f-pw", Name: "pw", Label: "Password"}, "current-password"))
	if got, _ := pw.Find("input#f-pw").Attr("type"); got != "password" {
		t.Errorf("password type = %q", got)
	}
	tg := pw.Find(`button[type=button][data-toggle-password]`)
	if tg.Length() != 1 || strings.TrimSpace(tg.Text()) != "Show password" {
		t.Errorf("toggle: %d %q", tg.Length(), tg.Text())
	}
	ta := uitest.Render(t, components.Textarea(components.FieldProps{ID: "f-n", Name: "n", Label: "Note"}, 1000))
	if got, _ := ta.Find("textarea#f-n").Attr("maxlength"); got != "1000" {
		t.Errorf("maxlength = %q", got)
	}
	if got := strings.TrimSpace(ta.Find("[data-counter]").Text()); got != "0 of 1,000 characters" {
		t.Errorf("counter = %q", got)
	}
	code := uitest.Render(t, components.CodeInput(components.FieldProps{ID: "f-g", Name: "g", Label: "GSTIN"}, components.GSTIN)).Find("input#f-g")
	for attr, want := range map[string]string{"autocapitalize": "characters", "spellcheck": "false", "hx-post": "/ui/validate/gstin", "hx-trigger": "change"} {
		if got, _ := code.Attr(attr); got != want {
			t.Errorf("code %s = %q, want %q", attr, got, want)
		}
	}
	pin := uitest.Render(t, components.CodeInput(components.FieldProps{ID: "f-pin", Name: "pin", Label: "Pincode"}, components.Pincode)).Find("input#f-pin")
	if got, _ := pin.Attr("inputmode"); got != "numeric" {
		t.Errorf("pincode inputmode = %q", got)
	}
}
