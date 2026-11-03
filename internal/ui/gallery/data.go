package gallery

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// priya is the user the gallery signs everyone in as.
var priya = auth.Principal{ID: 2, Name: "Priya Shah"}

// galleryIDs are the seeded sample documents, one per state plus the
// 500-line draft.
var galleryIDs = docscreen.SampleIDs{
	Draft: 42, Pending: 43, Approved: 44, Rejected: 45, Cancelled: 46, Closed: 47, Lines500: 500,
}

// shell is the fake app shell around every gallery page.
func shell(r *http.Request) ui.Shell {
	return ui.Shell{
		AppName:    brand.Load().AppName,
		User:       "Priya Shah",
		CSRF:       "gallery",
		Path:       r.URL.Path,
		InboxCount: 3,
		SignedIn:   true,
		Nav: []ui.NavItem{
			{Label: "Gallery", Href: "/_gallery", Icon: "image"},
			{Label: "Sample orders", Href: "/doc/" + docscreentest.Sample, Icon: "file-text"},
			{Label: "Buttons", Href: "/_gallery/buttons", Icon: "check"},
			{Label: "Fields", Href: "/_gallery/fields", Icon: "pencil"},
			{Label: "Lists", Href: "/_gallery/datalist", Icon: "file"},
			{Label: "Lines", Href: "/_gallery/lines", Icon: "indian-rupee"},
			{Label: "Wizard", Href: "/_gallery/wizard-1", Icon: "arrow-down"},
			{Label: "Uploads", Href: "/_gallery/upload", Icon: "upload"},
		},
		New: []ui.NavItem{
			{Label: "Sample order", Href: "/doc/" + docscreentest.Sample + "/new", Icon: "shopping-cart"},
			{Label: "Expense claim", Href: "/_gallery/wizard-1", Icon: "receipt-indian-rupee"},
			{Label: "Party", Href: "/_gallery/fields", Icon: "handshake"},
		},
	}
}

// stateDocs are the seeded documents in every state.
var stateDocs = []struct {
	id    int64
	state doctype.State
}{
	{galleryIDs.Draft, doctype.StateDraft},
	{galleryIDs.Pending, doctype.StatePending},
	{galleryIDs.Approved, doctype.StateApproved},
	{galleryIDs.Rejected, doctype.StateRejected},
	{galleryIDs.Cancelled, doctype.StateCancelled},
	{galleryIDs.Closed, doctype.StateClosed},
	{galleryIDs.Lines500, doctype.StateDraft},
}

func sampleInfo(id int64, s doctype.State) doctype.Info {
	info := doctype.Info{
		DocType: docscreentest.Sample, DocLabel: "Sample Order", ID: id, State: s,
		Display: "Draft " + strconv.FormatInt(id, 10), Party: uitest.LongName100, CreatedBy: priya.ID,
	}
	if s != doctype.StateDraft {
		info.Number = "SO/26-27/" + uitest.LongCode60[:5] + strconv.FormatInt(id, 10)
		info.Display = info.Number
	}
	return info
}

// docDeps seeds docfake with the sample and Hooked documents and builds the
// docscreen dependencies: Priya holds every right on the sample DocType, and
// on Hooked with its module (seedHooked).
func docDeps() docscreen.Deps {
	docscreentest.RegisterSample()
	docscreentest.SetMaster(docscreentest.Supplier, 1, doctype.MasterInfo{Code: uitest.LongCode60, Name: uitest.LongName100})
	docscreentest.SetMaster(docscreentest.Item, 1, doctype.MasterInfo{Code: "ST-001", Name: uitest.LongDevanagari})

	docs := docfake.New()
	var rows []docscreen.ListRow
	for _, d := range stateDocs {
		info := sampleInfo(d.id, d.state)
		values := map[string]string{
			"supplier_id": "1", "order_date": "2026-10-24", "priority": "Urgent",
			"remarks": uitest.LongError, "amount": "999999999.99",
		}
		n := 3
		if d.id == galleryIDs.Lines500 {
			n = 500
		}
		lines := make([]map[string]string, n)
		for i := range lines {
			lines[i] = map[string]string{"item_id": "1", "qty": "100000", "rate": "999999.9999"}
		}
		docscreentest.Seed(docs, info, doc.Record{Version: 1, Values: values, Lines: lines})
		rows = append(rows, docscreen.ListRow{Info: info, Values: values})
	}
	// The sweep's 409 screen saves the draft with an old version
	// (docscreen.Screens); its 422 screen is refused by UI's own parsers
	// before DOC is called.
	docs.SetResult(docscreentest.Sample, galleryIDs.Draft, doctype.ActionSave, doc.Result{}, &doc.Error{
		Kind:    doc.KindStale,
		Message: "Someone else changed Draft 42 after you opened it. Reload it and make your changes again.",
	})

	az := authfake.New()
	for _, r := range auth.AllRights {
		az.Grant("Buyer", docscreentest.Sample, r, auth.ScopeAll)
	}
	rows = append(rows, seedHooked(docs, az)...)
	az.AddRole(priya.ID, "Buyer")

	return docscreen.Deps{
		DB:        &docscreentest.Reader{},
		Engine:    docs,
		Auth:      az,
		Approval:  allowAll{},
		Lister:    searchLister{rows},
		Extras:    docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
		Today:     func() (string, string) { return "24-10-2026", "23-10-2026" },
		InboxURL:  "/approvals",
		PrintURL:  func(name string, id int64) string { return "/print/" + name + "/" + strconv.FormatInt(id, 10) },
		Modules:   map[string]docscreen.Module{docscreentest.Hooked: hookedModule()},
		// The gallery viewer may read every seeded record. Hooked's fixture
		// Link targets aren't registered here, so docscreen shows them as
		// documents, which need this check to show at all (CD-321).
		DocLinkRead: func(context.Context, *db.Tx, auth.Principal, string, int64) error { return nil },
	}
}

// allowAll lets everyone approve: the gallery shows no APR refusal.
type allowAll struct{}

func (allowAll) CheckApprover(context.Context, *db.Tx, auth.Principal, string, int64) error {
	return nil
}

// searchLister lists the DocType's seeded rows whose display or party
// contains the search text, so the "list empty" screen is empty.
type searchLister struct{ rows []docscreen.ListRow }

func (l searchLister) List(_ context.Context, _ auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	var out []docscreen.ListRow
	for _, r := range l.rows {
		if r.Info.DocType == dt.Name && (strings.Contains(r.Info.Display, q.Search) || strings.Contains(r.Info.Party, q.Search)) {
			out = append(out, r)
		}
	}
	return types.Result[docscreen.ListRow]{Total: len(out), Rows: out}, nil
}

// pickRows are the picker's search results: long rows and similar ones.
var pickRows = []components.PickRow{
	{ID: 1, Code: "ST-001", Name: "Steel rod 12 mm", Line: "Raw material · kg"},
	{ID: 2, Code: "ST-002", Name: "Stainless steel sheet 2 mm", Line: "Raw material · sheet"},
	{ID: 3, Code: uitest.LongCode60, Name: uitest.LongName100, Line: uitest.LongDevanagari},
	{ID: 4, Code: "BL-010", Name: "Bolt M8 × 40 mm", Line: "Fasteners · box of 100"},
}

// hostileName is markup as a name: every picker shows it as text.
const hostileName = "<img src=x onerror=alert(1)>"

// search returns the rows whose code or name contains q, ignoring case.
func search(q string) []components.PickRow {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []components.PickRow
	for _, r := range pickRows {
		if strings.Contains(strings.ToLower(r.Code+" "+r.Name), q) {
			out = append(out, r)
		}
	}
	return out
}

var (
	deliveryOptions = []components.Option{
		{Value: "site", Label: "To the site", Hint: uitest.LongName100},
		{Value: "office", Label: "To the office"},
		{Value: "pickup", Label: "We collect it"},
	}
	docOptions = []components.Option{
		{Value: "invoice", Label: "Tax invoice"},
		{Value: "challan", Label: "Delivery challan"},
		{Value: "test", Label: "Test certificate", Hint: uitest.LongDevanagari},
	}
	// orderOptions are radios with extra lines and a bar (UI v2 §5.6, E7).
	orderOptions = []components.Option{
		{Value: "1", Label: "PO/26-27/00001 · " + uitest.LongName100, Lines: []string{uitest.LongName100, "1 of 3 lines pending · ordered 24-10-2026"}, LineIcon: "map-pin", Bar: &components.Meter{Value: 1, Max: 3}},
		{Value: "2", Label: "PO/26-27/00004 · Bharat Tools", Lines: []string{"Pune Warehouse", "Fully received · ordered 20-10-2026"}, LineIcon: "map-pin", Bar: &components.Meter{Value: 0, Max: 2}},
	}
	priorityOptions = []components.Option{{Value: "Normal", Label: "Normal"}, {Value: "Urgent", Label: "Urgent"}}
	expenseOptions  = []components.Option{{Value: "fuel", Label: "Fuel"}, {Value: "travel", Label: "Local travel"}, {Value: "food", Label: "Food and refreshments"}, {Value: "stay", Label: "Stay"}, {Value: "other", Label: uitest.LongName100}}
	termOptions     = []components.Option{{Value: "0", Label: "Advance"}, {Value: "30", Label: "30 days"}, {Value: "60", Label: uitest.LongName100}}
)

// sampleList is a DataList of worst-case rows, or with no rows its empty state.
func sampleList(empty bool) components.DataListProps {
	p := components.DataListProps{
		Label: "Sample orders",
		Columns: []components.Column{
			{Key: "number", Label: "Number", Priority: components.PTitle, Code: true},
			{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
			{Key: "state", Label: "State", Priority: components.PState},
			{Key: "date", Label: "Order date", Priority: components.PMeta, NoWrap: true},
			{Key: "gstin", Label: "GSTIN", Priority: components.PMeta, NoWrap: true},
			{Key: "hindi", Label: "Name in Hindi", Priority: components.PLow},
		},
		Empty: components.EmptyList("No sample orders yet", "Sample orders you add show up here."),
	}
	if empty {
		return p
	}
	values := map[string]string{"number": uitest.LongCode60, "amount": uitest.MaxMoney, "date": "24-10-2026", "gstin": "29AAFCV2449G1Z5", "hindi": uitest.LongDevanagari}
	p.Rows = []components.Row{
		{Href: "/_gallery/datalist", Party: uitest.LongName100, Values: values, State: doctype.StatePending},
		{Href: "/_gallery/datalist", Values: values, State: doctype.StateApproved},
		{Href: "/_gallery/datalist", Party: uitest.LongDevanagari, Values: map[string]string{"number": "Draft 42", "amount": "₹0.00"}, State: doctype.StateDraft},
		{Href: "/_gallery/datalist", Party: "Shree Steel Traders", Values: map[string]string{"number": "PO/0043", "amount": "₹1,200.00", "date": "24-10-2026", "hindi": uitest.LongDevanagari}, State: doctype.StateApproved}, // blank GSTIN between two values
	}
	return p
}

// noStateList is a DataList with no state column (a non-submittable
// DocType), with worst-case values in every place and one row each with a
// blank first, middle and last value.
func noStateList() components.DataListProps {
	full := map[string]string{"code": uitest.LongCode60, "gstin": "29AAFCV2449G1Z5", "city": uitest.LongName100, "hindi": uitest.LongDevanagari}
	return components.DataListProps{
		Label: "Sample suppliers",
		Columns: []components.Column{
			{Key: "code", Label: "Code", Priority: components.PTitle, Code: true},
			{Key: "gstin", Label: "GSTIN", Priority: components.PMeta, NoWrap: true},
			{Key: "city", Label: "City", Priority: components.PMeta},
			{Key: "hindi", Label: "Name in Hindi", Priority: components.PLow},
		},
		Rows: []components.Row{
			{Href: "/_gallery/datalist", Party: uitest.LongName100, Values: full},
			{Href: "/_gallery/datalist", Values: map[string]string{"code": "SUP-002", "city": uitest.LongName100, "hindi": uitest.LongDevanagari}},
			{Href: "/_gallery/datalist", Party: uitest.LongDevanagari, Values: map[string]string{"code": "SUP-003", "gstin": "29AAFCV2449G1Z5", "hindi": uitest.LongDevanagari}},
			{Href: "/_gallery/datalist", Party: "Shree Steel Traders", Values: map[string]string{"code": "SUP-004", "gstin": "29AAFCV2449G1Z5", "city": "Pune"}},
		},
	}
}

// lineCards are the first n of 500 worst-case lines.
func lineCards(n int, editable bool) []components.LineCard {
	out := make([]components.LineCard, n)
	for i, l := range uitest.Lines(n) {
		no := i + 1
		c := components.LineCard{No: no, Item: l.Item, Code: uitest.LongCode60, QtyRate: l.Qty + " × " + l.Rate, Amount: l.Amount, Tax: "GST 18%"}
		if editable {
			c.ChangeURL, c.RemoveURL = "/_gallery/lines", "/_gallery/lines"
			if no > 1 {
				c.MoveUpURL = "/_gallery/lines"
			}
			if no < n {
				c.MoveDownURL = "/_gallery/lines"
			}
		}
		out[i] = c
	}
	return out
}

// gridRows are n line grid rows.
func gridRows(n int) []components.LineGridRow {
	out := make([]components.LineGridRow, n)
	for i := range out {
		// Line 1 has no item yet, so its picker shows the search.
		item := &pickRows[0]
		if i == 0 {
			item = nil
		}
		out[i] = gridRow(i+1, item, "1,00,000", "")
		if i > 0 {
			out[i].MoveUpURL = "/_gallery/lines"
		}
		if i < n-1 {
			out[i].MoveDownURL = "/_gallery/lines"
		}
	}
	// Line 2, with both move buttons, holds the largest rate and line
	// amount the app accepts, so the widest row is checked (M0-35).
	if n > 1 {
		out[1] = largestGridRow(out[1])
	}
	return out
}

// largestRate is types.MaxRate as typed, with all four decimals.
const largestRate = "10,00,00,000.0000"

// largestGridRow is row r with the largest rate and line amount, and its
// quantity, 1,00,000, with a unit suffix (M0-40).
func largestGridRow(r components.LineGridRow) components.LineGridRow {
	id := "grid-" + strconv.Itoa(r.No)
	cells := slices.Clone(r.Cells)
	cells[1].Input = components.QtyInput(components.FieldProps{ID: id + "-qty", Name: "qty", Label: "Quantity", Value: "1,00,000"}, false, "kg")
	cells[3].Input = components.RateInput(components.FieldProps{ID: id + "-rate", Name: "rate", Label: "Rate", Value: largestRate})
	r.Cells, r.Amount = cells, types.FormatINR(types.MaxMoney)
	return r
}

// gridRow is line no of the grid with its item, quantity and quantity error.
func gridRow(no int, item *components.PickRow, qty, errText string) components.LineGridRow {
	id := "grid-" + strconv.Itoa(no)
	return components.LineGridRow{
		No: no,
		Cells: []components.GridCell{
			{Label: "Item", Input: components.Picker(components.PickerProps{
				Field:     components.FieldProps{ID: id + "-item", Label: "Item"},
				SearchURL: searchURL(id + "-item-list"), HiddenName: "item_id", ListboxID: id + "-item-list",
				Chosen: item,
			})},
			{Label: "Quantity", Input: components.QtyInput(components.FieldProps{ID: id + "-qty", Name: "qty", Label: "Quantity", Value: qty, Error: errText}, false, "")},
			{Label: "Unit", Input: gridText("kg")},
			{Label: "Rate", Input: components.RateInput(components.FieldProps{ID: id + "-rate", Name: "rate", Label: "Rate", Value: "9,99,999.9999"})},
			{Label: "Tax", Input: gridText("GST 18%")},
		},
		Amount:    uitest.MaxMoney,
		PostURL:   "/_gallery/lines/rows/" + strconv.Itoa(no),
		RemoveURL: "/_gallery/lines",
	}
}

// sampleRows are row editor rows numbered first to last, with worst-case
// values. A row number shows on one page once, so its ids stay unique.
func sampleRows(first, last int) []components.RowsRow {
	var out []components.RowsRow
	for no := first; no <= last; no++ {
		id, name := "grid-"+strconv.Itoa(no)+"-", "lines["+strconv.Itoa(no)+"]."
		errText := ""
		if no == 2 {
			errText = uitest.LongError
		}
		out = append(out, components.RowsRow{No: no, Cells: []components.GridCell{
			{Label: "Unit", Input: components.TextInput(components.FieldProps{ID: id + "unit", Name: name + "unit", Label: "Unit", Value: uitest.LongName100})},
			{Label: "Code", Input: components.TextInput(components.FieldProps{ID: id + "code", Name: name + "code", Label: "Code", Value: uitest.LongCode60, Error: errText})},
			{Label: "Pack", Input: components.TextInput(components.FieldProps{ID: id + "pack", Name: name + "pack", Label: "Pack", Value: uitest.LongName100})},
			{Label: "Factor", Input: components.RateInput(components.FieldProps{ID: id + "factor", Name: name + "factor", Label: "Factor", Value: "9,99,999.999999"}), Wide: true},
			{Label: "Amount", Input: components.MoneyInput(components.FieldProps{ID: id + "amount", Name: name + "amount", Label: "Amount", Value: "99,99,99,999.99"}), Wide: true},
		}})
	}
	return out
}

// searchURL is the picker search for the listbox with id list.
func searchURL(list string) string { return "/_gallery/items?list=" + list }

// vendorNoun names the quick-create page's master.
var vendorNoun = components.Noun{One: "vendor", Many: "vendors"}

// quickList is the listbox of the quick-create page's picker, whose search
// offers New vendor (UI v2 §5.8).
const quickList = "f-new-vendor-list"

// tileOrders is every mix of short and wide options over five places.
func tileOrders() [][]components.Option {
	var out [][]components.Option
	for mix := range 1 << 5 {
		var opts []components.Option
		for place := range 5 {
			label := "Fuel"
			if mix>>place&1 == 1 {
				label = "Site miscellaneous"
			}
			label += " " + strconv.Itoa(place)
			opts = append(opts, components.Option{Value: label, Label: label})
		}
		out = append(out, opts)
	}
	return out
}

var attachments = []components.Attachment{
	{ID: 1, Kind: "image", Name: uitest.LongName100 + ".jpg", Size: "2.4 MB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:30", URL: brand.AppLogoURL, RemoveURL: "/_gallery/upload"},
	{ID: 2, Kind: "heic", Name: "IMG_0042.HEIC", Size: "3.1 MB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:31", URL: "/_gallery/upload", RemoveURL: "/_gallery/upload"},
	{ID: 3, Kind: "pdf", Name: uitest.LongCode60 + ".pdf", Size: "180 KB", UploadedBy: uitest.LongName100, UploadedAt: "24-10-2026 10:32", URL: "/_gallery/upload"},
}

// The fields page's info tips: one on a text input, one on a radio group.
const (
	codeTip     = "A short name you can type to find this party fast, up to 20 letters or digits."
	deliveryTip = "Goods sent to the site need a gate pass; goods to the office don't."
)

// docDisplayStep is one lifecycle step with a date (dd-mm).
func docDisplayStep(word string, m components.LifeMark) components.LifeStep {
	s := components.LifeStep{Label: word, Mark: m}
	if m != components.LifeTodo {
		s.Date = "28-10"
	}
	return s
}

// docDisplaySteps are the real lifecycles, the six-step ones the longest row
// (UI v2 §5.13 item 4), each word of every state in it.
var docDisplaySteps = [][]components.LifeStep{
	{docDisplayStep("Draft", components.LifeCurrent), docDisplayStep("Pending approval", components.LifeTodo), docDisplayStep("Approved", components.LifeTodo), docDisplayStep("Sent", components.LifeTodo), docDisplayStep("Received", components.LifeTodo)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Rejected", components.LifeRejected), docDisplayStep("Approved", components.LifeTodo), docDisplayStep("Sent", components.LifeTodo), docDisplayStep("Received", components.LifeTodo)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Pending approval", components.LifeDone), docDisplayStep("Approved", components.LifeDone), docDisplayStep("Sent", components.LifeDone), {Label: "Received", Mark: components.LifeCurrent, Note: "Part"}},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Pending approval", components.LifeDone), docDisplayStep("Approved", components.LifeDone), docDisplayStep("Sent", components.LifeDone), {Label: "Received", Mark: components.LifeDone, Note: "Part"}, docDisplayStep("Cancelled", components.LifeEnded)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Pending approval", components.LifeDone), docDisplayStep("Approved", components.LifeDone), docDisplayStep("Sent", components.LifeDone), docDisplayStep("Received", components.LifeDone), docDisplayStep("Closed", components.LifeEnded)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Pending approval", components.LifeDone), docDisplayStep("Approved", components.LifeDone), docDisplayStep("Paid", components.LifeCurrent)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Approved", components.LifeCurrent)},
	{docDisplayStep("Draft", components.LifeDone), docDisplayStep("Submitted", components.LifeDone), docDisplayStep("Closed", components.LifeEnded)},
}

// docDisplayAmounts are amounts of every length around amountClass's steps,
// up to 500 lines of the largest line amount.
var docDisplayAmounts = []string{
	"₹0.00", "₹1,250.00", "₹5,00,000.00", "₹99,99,999.00", "₹1,00,00,000.00", uitest.MaxMoney, "₹90,32,30,910.00",
	"₹1,24,43,14,336.88", "₹10,00,00,00,000.00", "₹99,99,99,99,999.99", "₹5,00,00,00,00,000.00", "₹50,00,00,00,00,000.00",
}

// docDisplayPairs are totals and detail rows, money and text mixed.
var docDisplayPairs = []components.Pair{
	{Label: "Vendor", Value: "Shree Steel Traders"},
	{Label: "Taxable value", Value: uitest.MaxMoney, Num: true},
	{Label: "CGST", Value: "₹1,250.00", Num: true},
	{Label: "Place of supply", Value: "Maharashtra (27)"},
	{Label: "Total", Value: uitest.MaxMoney, Num: true},
	{Label: "Less advance", Value: "₹0.00", Num: true},
	{Label: "To pay", Value: uitest.MaxMoney, Num: true},
	{Label: "Note", Value: uitest.LongName100},
	{Label: "Period", Value: "24-10-2026 to 30-10-2026"},
	{Label: "Against", Value: "PO/26-27/00001 dated 24-10-2026"},
	{Label: "Sent", Value: "Sent to vendor on 24-10-2026 by WhatsApp (Priya Shah) and again on 25-10-2026 by courier"},
}
