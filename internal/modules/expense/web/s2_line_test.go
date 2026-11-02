package expenseweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// The expense line page, Module.Line (plan MOD-EXP S2; UX §5.5; CD-270,
// CD-271 G4 and G9, CD-275). Every case fails before S2: claimLine returns
// nil, so the page is docscreen's generic line form, with no receipt area,
// no #line-form, Save line inside the form and none of §5.5's controls.

// s2WantBillDate checks EUX-047 on page: `Use the bill date.` is plain text
// inside the `Bill in company GSTIN` disclosure (open as open says), after
// `The bill shows our GSTIN` and before `Supplier GSTIN`, once on the page,
// never a field hint and never the target of any aria-describedby.
func s2WantBillDate(t *testing.T, what string, page *goquery.Document, open bool) {
	t.Helper()
	gst := s2Details(s2LineForm(t, page), s2GSTSummary)
	if gst.Length() != 1 {
		t.Fatalf("%s: %d %q disclosures in the line form, want 1", what, gst.Length(), s2GSTSummary)
	}
	if s2Open(gst) != open {
		t.Errorf("%s: %q open = %v, want %v", what, s2GSTSummary, s2Open(gst), open)
	}
	if n := strings.Count(s2Text(page.Selection), s2BillDate); n != 1 {
		t.Errorf("%s: %q appears %d times on the page, want once", what, s2BillDate, n)
	}
	text := s2Text(gst)
	tick, at, supplier := strings.Index(text, s2TickOption), strings.Index(text, s2BillDate), strings.Index(text, "Supplier GSTIN")
	if at < 0 || tick < 0 || supplier < 0 || tick >= at || at >= supplier {
		t.Errorf("%s: in the disclosure %q comes at %d, %q at %d, Supplier GSTIN at %d; want it right after the tick (%q)", what, s2BillDate, at, s2TickOption, tick, supplier, text)
	}
	el := gst.Find("*").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.Children().Length() == 0 && s2Text(s) == s2BillDate
	})
	if el.Length() != 1 {
		t.Fatalf("%s: %d elements hold exactly %q, want 1", what, el.Length(), s2BillDate)
	}
	if el.HasClass("field__hint") || el.Closest("label, legend").Length() > 0 {
		t.Errorf("%s: %q is a hint, label or legend, want plain text", what, s2BillDate)
	}
	page.Find("[aria-describedby]").Each(func(_ int, s *goquery.Selection) {
		for _, id := range strings.Fields(s.AttrOr("aria-describedby", "")) {
			if strings.Contains(s2Text(page.Find("#"+id)), s2BillDate) {
				t.Errorf("%s: aria-describedby %q of <%s name=%q> points at %q", what, id, goquery.NodeName(s), s.AttrOr("name", ""), s2BillDate)
			}
		}
	})
}

// UX §5.5 on a new line of a claim with no receipt yet: the receipt area
// first, outside the line form, posting to the module route with the page's
// version and disabling Save line while it uploads (UX-E5, EUX-030); then
// Type, Amount, Date, Note and the closed disclosures; Save line in the
// sticky bar (UX-E4). Two branch GSTINs, so Billed to is drawn (EUX-045).
func TestExpLine_NewPage(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	v := e.version(t, c.ID)
	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")
	form := s2LineForm(t, page)

	// Photo first, outside the line form (UX-E1).
	if first := page.Find("#receipt, #line-form").First().AttrOr("id", ""); first != "receipt" {
		t.Errorf("the first of #receipt and #line-form is %q, want the receipt area first", first)
	}
	for _, sel := range []string{"#receipt", "#receipt-files"} {
		if page.Find(sel).Length() != 1 {
			t.Errorf("the page has %d %s, want 1", page.Find(sel).Length(), sel)
		}
		if form.Find(sel).Length() != 0 || page.Find(sel).ParentsFiltered("details").Length() != 0 {
			t.Errorf("%s sits inside the line form or a disclosure, want it outside (UX-E1)", sel)
		}
	}
	// UI v2 (E7, §5.9): the receipt card, then the line form's card.
	receipt := s2Card(t, page.Selection, "Receipt", "camera", s2UploadHint)
	if receipt.Find("#receipt").Length() != 1 || receipt.Find("#receipt-files").Length() != 1 || receipt.Closest("form").Length() != 0 {
		t.Errorf("the Receipt card doesn't hold #receipt and #receipt-files outside the line form")
	}
	spent := s2Card(t, form, "What you spent", "receipt-indian-rupee", "Type, amount and the day on the bill.")
	for _, n := range []string{"expense_type_id", "amount", "expense_date"} {
		if spent.Find(`[name="`+n+`"]`).Length() == 0 {
			t.Errorf("%s isn't in the What you spent card", n)
		}
	}
	if more := s2Details(spent, s2MoreLine); more.Length() != 1 || spent.Children().Last().Get(0) != more.Get(0) {
		t.Errorf("%q isn't the What you spent card's last part", s2MoreLine)
	}
	up := page.Find("#receipt")
	if got := s2Text(up.Find("legend").First()); got != "Receipt" {
		t.Errorf("the upload's legend is %q, want Receipt", got)
	}
	if got := s2Text(up.Find(".field__hint").First()); got != s2UploadHint {
		t.Errorf("the upload's hint is %q, want %q", got, s2UploadHint)
	}
	action := "/modules/expense/claims/" + strconv.FormatInt(c.ID, 10) + "/receipt?version=" + strconv.Itoa(v)
	forms := up.Find("form")
	if forms.Length() != 2 {
		t.Fatalf("the receipt area has %d forms, want Take photo's and Choose file's", forms.Length())
	}
	saveLine := page.Find("button").FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Text(s) == "Save line" })
	forms.Each(func(i int, f *goquery.Selection) {
		if got := f.AttrOr("hx-post", ""); got != action {
			t.Errorf("upload form %d posts to %q, want %q", i, got, action)
		}
		if got := f.AttrOr("hx-target", ""); got != "#receipt-files" {
			t.Errorf("upload form %d targets %q, want #receipt-files", i, got)
		}
		sel := f.AttrOr("hx-disabled-elt", "")
		if sel == "" || page.Find(sel).FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Text(s) == "Save line" }).Length() != 1 {
			t.Errorf("upload form %d disables %q while uploading, which doesn't reach Save line (UX-E5, EUX-030)", i, sel)
		}
	})
	if up.Find(`input[type="file"][capture="environment"]`).Length() != 1 {
		t.Errorf("the receipt area has no Take photo (rear camera) input")
	}
	if got := up.Find(`input[type="file"]:not([capture])`).AttrOr("accept", ""); got != "image/*,application/pdf" {
		t.Errorf("Choose file accepts %q, want image/*,application/pdf", got)
	}

	// Save line in the sticky bar, posting the line form (UX-E4).
	if saveLine.Length() != 1 {
		t.Fatalf("the page has %d Save line buttons, want 1", saveLine.Length())
	}
	if saveLine.AttrOr("form", "") != "line-form" || saveLine.ParentsFiltered(".actionbar").Length() != 1 || saveLine.ParentsFiltered("form").Length() != 0 {
		t.Errorf("Save line: form=%q, in the action bar %v, inside a form %v; want form=line-form in the bar, outside the form",
			saveLine.AttrOr("form", ""), saveLine.ParentsFiltered(".actionbar").Length() == 1, saveLine.ParentsFiltered("form").Length() != 0)
	}

	// The hidden hash the upload answer replaces, blank on a new line.
	if got := s2HiddenReceipt(t, form).AttrOr("value", "x"); got != "" {
		t.Errorf("a new line's receipt_sha256 is %q, want blank", got)
	}

	// Order: the fields every expense needs, then the choices (UX §5.5).
	var names []string
	form.Find("[name]").Each(func(_ int, s *goquery.Selection) {
		n := s.AttrOr("name", "")
		if n != "version" && n != "csrf_token" && n != "receipt_hash" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	})
	order := []string{"expense_type_id", "amount", "expense_date", "description", "no_receipt_reason", "company_bill",
		"billed_to_id", "supplier_gstin", "invoice_no", "tax_code_id", "hsn_sac", "gst_amount", "project_id", "site_id"}
	last := -1
	for _, n := range order {
		i := slices.Index(names, n)
		if i < 0 {
			t.Errorf("the line form has no %s (it has %q)", n, names)
			continue
		}
		if i < last {
			t.Errorf("%s comes before an earlier field of %q in %q", n, order, names)
		}
		last = i
	}

	// Type: radios of the enabled types by name, none chosen (CD-270 Q7).
	types := s2Options(form, "expense_type_id")
	if got, want := s2Labels(types), []string{"Food", "Fuel", "Lodging", "Site miscellaneous", "Travel"}; !slices.Equal(got, want) {
		t.Errorf("Type offers %q, want %q", got, want)
	}
	for _, o := range types {
		if o.Checked {
			t.Errorf("Type %q is checked on a new line, want none (no default)", o.Label)
		}
		if code := map[string]string{"Food": "FOOD", "Fuel": "FUEL", "Lodging": "LODGING", "Site miscellaneous": "SITE-MISC", "Travel": "TRAVEL"}[o.Label]; o.Value != strconv.FormatInt(e.F.ExpenseTypeIDs[code], 10) {
			t.Errorf("Type %q has value %q, want %s's id %d", o.Label, o.Value, code, e.F.ExpenseTypeIDs[code])
		}
	}
	if got := form.Find(`input[name="expense_type_id"]`).AttrOr("type", ""); got != "radio" {
		t.Errorf("Type is a %q input, want radios with 5 types", got)
	}
	if n := form.Find(`.segmented--tiles input[name="expense_type_id"]`).Length(); n != 5 {
		t.Errorf("%d of Type's 5 radios are tiles, want all (UI v2 §5.5 item 8)", n)
	}
	for _, f := range []struct{ name, label, hint string }{
		{"expense_type_id", "Type", ""},
		{"amount", "Amount", s2AmountHint},
		{"expense_date", "Date", ""},
		{"description", "Note", s2NoteHint},
		{"no_receipt_reason", "Why no receipt", s2ReasonHint},
		{"company_bill", "Bill in company GSTIN", s2TickHint},
		{"billed_to_id", "Billed to", s2BilledToHint},
		{"supplier_gstin", "Supplier GSTIN", ""},
		{"invoice_no", "Invoice no.", ""},
		{"tax_code_id", "Tax code", ""},
		{"hsn_sac", "HSN/SAC", ""},
		{"gst_amount", "GST on the bill", s2GSTHint},
		{"project_id", "Project", ""},
		{"site_id", "Site", ""},
	} {
		if got := s2Label(t, form, f.name); got != f.label {
			t.Errorf("%s's label is %q, want %q", f.name, got, f.label)
		}
		if want := f.name == "expense_type_id" || f.name == "amount" || f.name == "expense_date"; s2Required(t, form, f.name) != want {
			t.Errorf("%s marked required = %v, want %v (CD-341 (2))", f.name, !want, want)
		}
		if f.hint != "" {
			if got := s2Hint(t, form, f.name); got != f.hint {
				t.Errorf("%s's hint is %q, want %q", f.name, got, f.hint)
			}
		}
	}

	// Date: today, with the Today and Yesterday shortcuts.
	if got := form.Find(`input[name="expense_date"]`).AttrOr("value", ""); got != s2Today {
		t.Errorf("Date is %q on a new line, want today %s", got, s2Today)
	}
	shortcuts := map[string]string{}
	s2Field(t, form, "expense_date").Find("[data-set-date]").Each(func(_ int, s *goquery.Selection) {
		shortcuts[s2Text(s)] = s.AttrOr("data-set-date", "")
	})
	if shortcuts["Today"] != s2Today || shortcuts["Yesterday"] != s2Yesterday {
		t.Errorf("Date's shortcuts are %v, want Today %s and Yesterday %s", shortcuts, s2Today, s2Yesterday)
	}
	if s2Hint(t, form, "expense_date") == s2BillDate {
		t.Errorf("Date's hint is %q; the bill-date text belongs in the GSTIN disclosure (CD-271 G9)", s2BillDate)
	}

	// The disclosures, all closed and inside the one More details; no
	// same-receipt choice without another line's receipt.
	if got, want := s2Summaries(form), []string{s2MoreLine, s2NoReceipt, s2GSTSummary, s2ProjSummary}; !slices.Equal(got, want) {
		t.Errorf("the line form's disclosures are %q, want %q", got, want)
	}
	if n := s2Details(form, s2MoreLine).Find("details").Length(); n != 3 {
		t.Errorf("%d disclosures inside %q, want No receipt, GSTIN and Project or site", n, s2MoreLine)
	}
	form.Find("details").Each(func(_ int, d *goquery.Selection) {
		if s2Open(d) {
			t.Errorf("%q is open on a new line, want closed", s2Text(d.ChildrenFiltered("summary")))
		}
	})
	for summary, names := range map[string][]string{
		s2NoReceipt:   {"no_receipt_reason"},
		s2GSTSummary:  {"company_bill", "billed_to_id", "supplier_gstin", "invoice_no", "tax_code_id", "hsn_sac", "gst_amount"},
		s2ProjSummary: {"project_id", "site_id"},
	} {
		for _, n := range names {
			if s2Details(form, summary).Find(`[name="`+n+`"]`).Length() == 0 {
				t.Errorf("%s isn't inside the %q disclosure", n, summary)
			}
		}
	}
	if form.Find(`textarea[name="no_receipt_reason"]`).Length() != 1 {
		t.Errorf("Why no receipt isn't a textarea")
	}
	tick := s2Options(form, "company_bill")
	if len(tick) != 1 || tick[0].Label != s2TickOption || tick[0].Checked {
		t.Errorf("the GSTIN tick is %+v, want one unticked %q", tick, s2TickOption)
	}
	s2WantBillDate(t, "new line", page, false)

	// Billed to with two branch GSTINs: by code, none chosen (EUX-045).
	billed := s2Options(form, "billed_to_id")
	want := []s2Option{
		{Label: "BLR · Bengaluru Site Office", Hint: "GSTIN 29AAFCV2449G1Z3", Value: strconv.FormatInt(e.id(t, `SELECT id FROM branch WHERE code = 'BLR'`), 10)},
		{Label: "PUNE · Pune Works", Hint: "GSTIN 27AAFCV2449G1Z7", Value: strconv.FormatInt(e.id(t, `SELECT id FROM branch WHERE code = 'PUNE'`), 10)},
	}
	if !slices.Equal(billed, want) {
		t.Errorf("Billed to offers %+v, want %+v", billed, want)
	}
}

// Billed to isn't drawn while the enabled branches carry one distinct GSTIN
// (BranchGSTINs is 1; CD-271 G4, EUX-046); the bill-date text stays in the
// disclosure (EUX-047).
func TestExpLine_BilledToOneGSTIN(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, q string }{
		{"BLR disabled", `UPDATE branch SET disabled = 1 WHERE code = 'BLR'`},
		{"BLR shares PUNE's GSTIN", `UPDATE branch SET gstin = '27AAFCV2449G1Z7' WHERE code = 'BLR'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := s2New(t)
			e.exec(t, tc.q)
			c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
			page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")
			if n := page.Find(`[name="billed_to_id"]`).Length(); n != 0 {
				t.Errorf("the page has %d billed_to_id controls, want none with one branch GSTIN", n)
			}
			if strings.Contains(s2Text(page.Selection), "Billed to") {
				t.Errorf("the page mentions Billed to with one branch GSTIN")
			}
			s2WantBillDate(t, tc.name, page, false)
		})
	}
}

// Type is radios up to 6 enabled types and a Select above 6, sorted by name,
// with no default; a disabled type isn't offered (UX §5.5, CD-270 Q7).
func TestExpLine_TypeChoices(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	for _, tc := range [][2]string{{"PARKING", "Parking"}, {"TOLL", "Toll"}} {
		e.exec(t, `INSERT INTO expense_type (code, name, created_at, updated_at) VALUES (?, ?, '2026-10-01T00:00:00.000Z', '2026-10-01T00:00:00.000Z')`, tc[0], tc[1])
	}
	e.exec(t, `UPDATE expense_type SET disabled = 1 WHERE code = 'LODGING'`)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	path := s2ClaimPath(c.ID) + "/lines/new"

	form := s2LineForm(t, e.page(t, s2Deepak, path))
	if got, want := s2Labels(s2Options(form, "expense_type_id")), []string{"Food", "Fuel", "Parking", "Site miscellaneous", "Toll", "Travel"}; !slices.Equal(got, want) {
		t.Errorf("with 6 enabled types Type offers radios %q, want %q", got, want)
	}

	e.exec(t, `UPDATE expense_type SET disabled = 0 WHERE code = 'LODGING'`)
	form = s2LineForm(t, e.page(t, s2Deepak, path))
	sel := form.Find(`select[name="expense_type_id"]`)
	if sel.Length() != 1 || form.Find(`input[name="expense_type_id"]`).Length() != 0 {
		t.Fatalf("with 7 enabled types: %d selects and %d inputs named expense_type_id, want one Select", sel.Length(), form.Find(`input[name="expense_type_id"]`).Length())
	}
	var labels []string
	sel.Find("option").Each(func(_ int, o *goquery.Selection) {
		if o.AttrOr("value", "") == "" {
			return
		}
		labels = append(labels, s2Text(o))
		if _, on := o.Attr("selected"); on {
			t.Errorf("Type %q is selected on a new line, want none", s2Text(o))
		}
	})
	if want := []string{"Food", "Fuel", "Lodging", "Parking", "Site miscellaneous", "Toll", "Travel"}; !slices.Equal(labels, want) {
		t.Errorf("the Type Select offers %q, want %q", labels, want)
	}
}

// Save line posts this one line from Line.Save: the uploaded receipt's hash,
// the type, amount, date and note; a company-bill line its tick, Billed to,
// supplier GSTIN, invoice and GST; then back to Expenses. Reopened, the
// ticked line has its disclosure open with the bill-date text (EUX-047), and
// a line with a reason has No receipt open.
func TestExpLine_Save(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	id := strconv.FormatInt(c.ID, 10)
	items := s2ClaimPath(c.ID) + "/edit/items"
	if rec := e.upload(t, s2Deepak, id, strconv.Itoa(e.version(t, c.ID)), s2File("bill.jpg", filestest.JPEG(4096))); rec.Code != http.StatusOK {
		t.Fatalf("upload bill.jpg: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	bill := e.FilesOf(t, expense.ClaimDocType, c.ID)[0]

	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")
	rec := e.postForm(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new", s2Base(t, page, bill.SHA256))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != items {
		t.Fatalf("Save line: status %d, Location %q; want 303 to %s (body %q)", rec.Code, rec.Header().Get("Location"), items, rec.Body.String())
	}
	got := e.line(t, c.ID, 2)
	for k, want := range map[string]string{
		"expense_type_id": strconv.FormatInt(e.F.ExpenseTypeIDs["FOOD"], 10), "amount": "450.00", "expense_date": "2026-10-24",
		"description": "Lunch at site canteen", "receipt_sha256": bill.SHA256, "no_receipt_reason": "", "company_bill": "false",
	} {
		if got[k] != want {
			t.Errorf("saved line 2 %s = %q, want %q", k, got[k], want)
		}
	}

	// A company bill with no receipt and a reason.
	page = e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")
	form := s2LineForm(t, page)
	pune := strconv.FormatInt(e.id(t, `SELECT id FROM branch WHERE code = 'PUNE'`), 10)
	post := s2Base(t, page, "")
	post.Set("expense_type_id", s2ValueOf(t, s2Options(form, "expense_type_id"), "Lodging"))
	post.Set("amount", "4500")
	post.Set("description", "")
	post.Set("no_receipt_reason", "Paper file")
	post.Set("company_bill", s2ValueOf(t, s2Options(form, "company_bill"), s2TickOption))
	post.Set("billed_to_id", s2ValueOf(t, s2Options(form, "billed_to_id"), "PUNE · Pune Works"))
	post.Set("supplier_gstin", "27AABCS1429B1ZU")
	post.Set("invoice_no", "INV-881")
	post.Set("gst_amount", "482.14")
	if rec := e.postForm(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new", post); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line (company bill): status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	got = e.line(t, c.ID, 3)
	for k, want := range map[string]string{
		"expense_type_id": strconv.FormatInt(e.F.ExpenseTypeIDs["LODGING"], 10), "amount": "4500.00", "receipt_sha256": "",
		"no_receipt_reason": "Paper file", "company_bill": "true", "billed_to_id": pune, "supplier_gstin": "27AABCS1429B1ZU",
		"invoice_no": "INV-881", "gst_amount": "482.14",
	} {
		if got[k] != want {
			t.Errorf("saved line 3 %s = %q, want %q", k, got[k], want)
		}
	}

	// Reopened: the tick, the branch and the reason are shown, their
	// disclosures open.
	page = e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/3")
	form = s2LineForm(t, page)
	s2WantBillDate(t, "ticked line reopened", page, true)
	if tick := s2Options(form, "company_bill"); len(tick) != 1 || !tick[0].Checked {
		t.Errorf("reopened line 3's tick is %+v, want ticked", tick)
	}
	for _, o := range s2Options(form, "billed_to_id") {
		if o.Checked != (o.Value == pune) {
			t.Errorf("reopened line 3: Billed to %q checked = %v, want only PUNE", o.Label, o.Checked)
		}
	}
	if d := s2Details(form, s2NoReceipt); !s2Open(d) || s2Text(d.Find(`textarea[name="no_receipt_reason"]`)) != "Paper file" {
		t.Errorf("reopened line 3: No receipt open %v holding %q, want open with Paper file", s2Open(d), s2Text(d.Find("textarea")))
	}
	if s2Open(s2Details(form, s2ProjSummary)) {
		t.Errorf("reopened line 3: Project or site is open with neither set")
	}
}

// Reopening a line that holds a receipt shows that file in #receipt-files
// (no Remove) with its hash in the line form, and the line's values.
func TestExpLine_ReopenWithReceipt(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	taxi := e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})["taxi.jpg"]

	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")
	form := s2LineForm(t, page)
	s2WantShown(t, "line 1 reopened", page, taxi)
	if got := s2HiddenReceipt(t, form).AttrOr("value", ""); got != taxi.SHA256 {
		t.Errorf("line 1's receipt_sha256 is %q, want %s", got, taxi.SHA256)
	}
	for _, o := range s2Options(form, "expense_type_id") {
		if o.Checked != (o.Label == "Travel") {
			t.Errorf("line 1: Type %q checked = %v, want only Travel", o.Label, o.Checked)
		}
	}
	if got := form.Find(`input[name="expense_date"]`).AttrOr("value", ""); got != s2Today {
		t.Errorf("line 1's Date is %q, want %s", got, s2Today)
	}
	if s2Open(s2Details(form, s2NoReceipt)) {
		t.Errorf("line 1 has a receipt and no reason, but No receipt is open")
	}
}

// s2WantShown checks that page's #receipt-files shows m: its thumbnail link
// to /files/{id}, with no Remove (UX §5.5).
func s2WantShown(t *testing.T, what string, page *goquery.Document, m files.Meta) {
	t.Helper()
	rf := page.Find("#receipt-files")
	href := "/files/" + strconv.FormatInt(m.ID, 10)
	if rf.Find(`a[href="`+href+`"] img`).Length() != 1 {
		t.Errorf("%s: #receipt-files doesn't show %s's thumbnail linking to %s (%q)", what, m.Name, href, s2Text(rf))
	}
	if strings.Contains(s2Text(rf), "Remove") {
		t.Errorf("%s: #receipt-files offers Remove", what)
	}
}

// After a 422 or 409 on Save line, Line.Top redraws the posted receipt and
// the form keeps its hash and the typed values; the next Save line stores
// the hash (UX-E1, EUX-035). A ticked post's 422 keeps the GSTIN disclosure
// open with the bill-date text (EUX-047).
func TestExpLine_RedrawKeepsReceipt(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	id := strconv.FormatInt(c.ID, 10)
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	if rec := e.upload(t, s2Deepak, id, strconv.Itoa(e.version(t, c.ID)), s2File("bill.jpg", filestest.JPEG(4096))); rec.Code != http.StatusOK {
		t.Fatalf("upload bill.jpg: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	bill := e.FilesOf(t, expense.ClaimDocType, c.ID)[0]
	base := s2Base(t, e.page(t, s2Deepak, newLine), bill.SHA256)

	kept := func(what string, page *goquery.Document, post url.Values) {
		t.Helper()
		s2WantShown(t, what, page, bill)
		form := s2LineForm(t, page)
		if got := s2HiddenReceipt(t, form).AttrOr("value", ""); got != bill.SHA256 {
			t.Errorf("%s: receipt_sha256 is %q, want the posted %s", what, got, bill.SHA256)
		}
		for _, n := range []string{"amount", "expense_date", "description"} {
			if got := form.Find(`[name="`+n+`"]`).AttrOr("value", ""); got != post.Get(n) {
				t.Errorf("%s: %s is %q, want the typed %q", what, n, got, post.Get(n))
			}
		}
		for _, o := range s2Options(form, "expense_type_id") {
			if o.Checked != (o.Value == post.Get("expense_type_id")) {
				t.Errorf("%s: Type %q checked = %v, want the posted type only", what, o.Label, o.Checked)
			}
		}
	}

	// 422: a future date.
	future := url.Values{}
	for k, v := range base {
		future[k] = v
	}
	future.Set("expense_date", "25-10-2026")
	rec := e.postForm(t, s2Deepak, newLine, future)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("future date: status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	page := s2Parse(t, rec)
	if !strings.Contains(s2Text(page.Selection), "Line 2: Date can't be in the future.") {
		t.Errorf("future date: the page doesn't say %q", "Line 2: Date can't be in the future.")
	}
	kept("422", page, future)

	// 422 on a ticked line: a bad supplier GSTIN.
	ticked := url.Values{}
	for k, v := range base {
		ticked[k] = v
	}
	form := s2LineForm(t, e.page(t, s2Deepak, newLine))
	ticked.Set("company_bill", s2ValueOf(t, s2Options(form, "company_bill"), s2TickOption))
	ticked.Set("billed_to_id", s2ValueOf(t, s2Options(form, "billed_to_id"), "PUNE · Pune Works"))
	ticked.Set("supplier_gstin", "27AABCS1429B1Z5")
	ticked.Set("invoice_no", "INV-881")
	rec = e.postForm(t, s2Deepak, newLine, ticked)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad GSTIN: status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	page = s2Parse(t, rec)
	const badGSTIN = "Line 2: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."
	if !strings.Contains(s2Text(page.Selection), badGSTIN) {
		t.Errorf("bad GSTIN: the page doesn't say %q", badGSTIN)
	}
	s2WantBillDate(t, "ticked 422", page, true)
	if got := s2LineForm(t, page).Find(`input[name="supplier_gstin"]`).AttrOr("value", ""); got != "27AABCS1429B1Z5" {
		t.Errorf("bad GSTIN: Supplier GSTIN is %q, want the typed 27AABCS1429B1Z5", got)
	}
	kept("ticked 422", page, ticked)

	// 409: someone saved the claim since the page was opened.
	if _, err := e.EditClaim(t, deepak, c.ID, expensetest.Values{"purpose": "Site visit, Pune"}, nil); err != nil {
		t.Fatalf("edit claim: %v", err)
	}
	rec = e.postForm(t, s2Deepak, newLine, base)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale Save line: status %d, want 409 (body %q)", rec.Code, rec.Body.String())
	}
	page = s2Parse(t, rec)
	if !strings.Contains(s2Text(page.Selection), s2MsgStale(c.ID)) {
		t.Errorf("stale Save line: the page doesn't say %q", s2MsgStale(c.ID))
	}
	kept("409", page, base)

	// The next Save line stores the hash.
	base.Set("version", strconv.Itoa(e.version(t, c.ID)))
	if rec := e.postForm(t, s2Deepak, newLine, base); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line after the refusals: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if got := e.line(t, c.ID, 2)["receipt_sha256"]; got != bill.SHA256 {
		t.Errorf("saved line 2's receipt_sha256 = %q, want %s", got, bill.SHA256)
	}
}

// Same receipt as another expense (spec §3.3, EUX-005): shown only when
// another line has a receipt; `No, a receipt of its own` first and checked,
// then one option per distinct hash, labelled with the lowest line holding
// it, hint `{type} · {date} · {amount}`. Picking one saves that line's hash,
// over a photo uploaded on the page; reopened, it is open and picked.
func TestExpLine_SameReceipt(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	food := strconv.FormatInt(e.F.ExpenseTypeIDs["FOOD"], 10)
	c := e.DraftClaim(t, deepak, nil, nil, expensetest.Values{"expense_type_id": food, "amount": "450.00"})
	metas := e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg", 2: "bill.jpg"})
	id := strconv.FormatInt(c.ID, 10)
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	wantLabels := []string{s2SameOwn, "Same receipt as line 1", "Same receipt as line 2"}

	page := e.page(t, s2Deepak, newLine)
	form := s2LineForm(t, page)
	if got, want := s2Summaries(form), []string{s2MoreSame, s2SameSummary, s2NoReceipt, s2GSTSummary, s2ProjSummary}; !slices.Equal(got, want) {
		t.Errorf("the line form's disclosures are %q, want %q", got, want)
	}
	same := s2Details(form, s2SameSummary)
	if s2Open(same) {
		t.Errorf("%q is open on a new line", s2SameSummary)
	}
	opts := s2Options(same, "same_as")
	want := []s2Option{
		{Label: s2SameOwn, Checked: true},
		{Label: "Same receipt as line 1", Hint: "Travel · 24-10-2026 · ₹1,000.00"},
		{Label: "Same receipt as line 2", Hint: "Food · 24-10-2026 · ₹450.00"},
	}
	if len(opts) != len(want) {
		t.Fatalf("same_as offers %+v, want %q", opts, wantLabels)
	}
	for i, o := range opts {
		if o.Label != want[i].Label || o.Hint != want[i].Hint || o.Checked != want[i].Checked {
			t.Errorf("same_as option %d = %q (hint %q, checked %v), want %q (hint %q, checked %v)", i, o.Label, o.Hint, o.Checked, want[i].Label, want[i].Hint, want[i].Checked)
		}
	}

	// A photo uploaded on this page, then line 2 picked: line 2's hash wins.
	if rec := e.upload(t, s2Deepak, id, strconv.Itoa(e.version(t, c.ID)), s2File("other.jpg", s2Bytes("other.jpg"))); rec.Code != http.StatusOK {
		t.Fatalf("upload other.jpg: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	var other files.Meta
	for _, m := range e.FilesOf(t, expense.ClaimDocType, c.ID) {
		if m.Name == "other.jpg" {
			other = m
		}
	}
	post := s2Base(t, page, other.SHA256)
	post.Set("same_as", s2ValueOf(t, opts, "Same receipt as line 2"))
	if rec := e.postForm(t, s2Deepak, newLine, post); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line with Same receipt as line 2: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if got := e.line(t, c.ID, 3)["receipt_sha256"]; got != metas["bill.jpg"].SHA256 {
		t.Errorf("line 3's receipt_sha256 = %q, want line 2's %s", got, metas["bill.jpg"].SHA256)
	}

	// Reopened, line 3 has the disclosure open with line 2 picked.
	form = s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/3"))
	same = s2Details(form, s2SameSummary)
	if !s2Open(same) || !s2Open(s2Details(form, s2MoreSame)) {
		t.Errorf("line 3 reopened: %q open %v, %q open %v, want both open with line 2 picked", s2SameSummary, s2Open(same), s2MoreSame, s2Open(s2Details(form, s2MoreSame)))
	}
	for _, o := range s2Options(same, "same_as") {
		if o.Checked != (o.Label == "Same receipt as line 2") {
			t.Errorf("line 3 reopened: %q checked = %v, want only line 2", o.Label, o.Checked)
		}
	}

	// Lines 2 and 3 share a hash: one option, for line 2.
	if got := s2Labels(s2Options(s2LineForm(t, e.page(t, s2Deepak, newLine)), "same_as")); !slices.Equal(got, wantLabels) {
		t.Errorf("with lines 2 and 3 on one receipt, same_as offers %q, want %q", got, wantLabels)
	}

	// Only this line has a receipt: no choice on its page; another line's
	// page offers it.
	c2 := e.DraftClaim(t, deepak, nil, nil, nil)
	e.withReceipts(t, c2.ID, map[int]string{1: "solo.jpg"})
	if n := s2Details(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c2.ID)+"/lines/1")), s2SameSummary).Length(); n != 0 {
		t.Errorf("line 1, the only line with a receipt, shows %q", s2SameSummary)
	}
	if got := s2Labels(s2Options(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c2.ID)+"/lines/2")), "same_as")); !slices.Equal(got, []string{s2SameOwn, "Same receipt as line 1"}) {
		t.Errorf("line 2 of the second claim: same_as offers %q, want %q", got, []string{s2SameOwn, "Same receipt as line 1"})
	}
}

// Type is marked required as tiles and as a Select (UI v2 §5.5 item 2), and
// only the Required fields are: Type, Amount and Date. Billed to, Same receipt
// and the no-receipt reason have no marker, as no field of theirs is Required.
func TestExpLine_RequiredMarkers(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})
	path := s2ClaimPath(c.ID) + "/lines/new"

	check := func(what string, form *goquery.Selection) {
		t.Helper()
		if !s2Required(t, form, "expense_type_id") {
			t.Errorf("%s: Type isn't marked required", what)
		}
		var names []string
		form.Find(`[aria-required]`).Each(func(_ int, s *goquery.Selection) {
			if n := s.AttrOr("name", ""); !slices.Contains(names, n) {
				names = append(names, n)
			}
		})
		slices.Sort(names)
		if want := []string{"amount", "expense_date", "expense_type_id"}; !slices.Equal(names, want) {
			t.Errorf("%s: aria-required on %q, want only %q", what, names, want)
		}
		if n := form.Find(".field__req").Length(); n != 3 {
			t.Errorf("%s: %d asterisks, want Type's, Amount's and Date's", what, n)
		}
	}

	form := s2LineForm(t, e.page(t, s2Deepak, path))
	if form.Find(`.segmented--tiles input[name="expense_type_id"]`).Length() == 0 {
		t.Fatal("Type isn't tiles with 5 types")
	}
	for _, n := range []string{"billed_to_id", "same_as", "no_receipt_reason"} {
		if form.Find(`[name="`+n+`"]`).Length() == 0 {
			t.Errorf("%s isn't on the page, so its missing marker proves nothing", n)
		}
	}
	check("tiles", form)

	for _, tc := range [][2]string{{"PARKING", "Parking"}, {"TOLL", "Toll"}} {
		e.exec(t, `INSERT INTO expense_type (code, name, created_at, updated_at) VALUES (?, ?, '2026-10-01T00:00:00.000Z', '2026-10-01T00:00:00.000Z')`, tc[0], tc[1])
	}
	form = s2LineForm(t, e.page(t, s2Deepak, path))
	if form.Find(`select[name="expense_type_id"]`).Length() != 1 {
		t.Fatal("Type isn't a Select with 7 types")
	}
	check("select", form)
}

// The no-receipt reason takes at most the field's 200 characters.
func TestExpLine_ReasonMaxLength(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	form := s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new"))
	if got := form.Find(`textarea[name="no_receipt_reason"]`).AttrOr("maxlength", ""); got != "200" {
		t.Errorf("the reason's maxlength is %q, want 200", got)
	}
}

// A picked same receipt, stored or posted, alone opens its disclosure and the
// outer More details; with none picked both stay closed. Every line here has
// nothing else that opens More details, so only the pick can open it.
func TestExpLine_SameReceiptOpensMore(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	shared := e.DraftClaim(t, deepak, nil, nil, nil)
	e.withReceipts(t, shared.ID, map[int]string{1: "bill.jpg", 2: "bill.jpg"})
	own := e.DraftClaim(t, deepak, nil, nil, nil)
	e.withReceipts(t, own.ID, map[int]string{1: "taxi.jpg", 2: "food.jpg"})
	sharedLine := s2ClaimPath(shared.ID) + "/lines/2"
	sharedNew := s2ClaimPath(shared.ID) + "/lines/new"

	want := func(what string, form *goquery.Selection, open bool) {
		t.Helper()
		same, more := s2Details(form, s2SameSummary), s2Details(form, s2MoreSame)
		if same.Length() != 1 || more.Length() != 1 {
			t.Fatalf("%s: %d %q and %d %q, want one each", what, same.Length(), s2SameSummary, more.Length(), s2MoreSame)
		}
		if s2Open(same) != open || s2Open(more) != open {
			t.Errorf("%s: same receipt open %v, More details open %v, want both %v", what, s2Open(same), s2Open(more), open)
		}
	}
	// redraw posts page's form with same_as and a future date, for a 422.
	redraw := func(what, path, sameAs string) *goquery.Selection {
		t.Helper()
		post := s2Base(t, e.page(t, s2Deepak, path), "")
		post.Del("description")
		post.Set("same_as", sameAs)
		post.Set("expense_date", "25-10-2026")
		rec := e.postForm(t, s2Deepak, path, post)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, want 422 (body %q)", what, rec.Code, rec.Body.String())
		}
		return s2LineForm(t, s2Parse(t, rec))
	}

	want("stored pick", s2LineForm(t, e.page(t, s2Deepak, sharedLine)), true)
	want("posted pick", redraw("posted pick", sharedNew, "1"), true)
	want("new line, none picked", s2LineForm(t, e.page(t, s2Deepak, sharedNew)), false)
	want("own receipt stored", s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(own.ID)+"/lines/2")), false)
	want("stored pick posted away", redraw("posted away", sharedLine, ""), false)
}
