package gallery

import (
	"context"
	"net/http"
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

// docDeps seeds docfake with the sample documents and builds the docscreen
// dependencies: Priya holds every right on the sample DocType.
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
	}
}

// allowAll lets everyone approve: the gallery shows no APR refusal.
type allowAll struct{}

func (allowAll) CheckApprover(context.Context, *db.Tx, auth.Principal, string, int64) error {
	return nil
}

// searchLister lists the seeded rows whose display or party contains the
// search text, so the "list empty" screen is empty.
type searchLister struct{ rows []docscreen.ListRow }

func (l searchLister) List(_ context.Context, _ auth.Principal, _ *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	var out []docscreen.ListRow
	for _, r := range l.rows {
		if strings.Contains(r.Info.Display, q.Search) || strings.Contains(r.Info.Party, q.Search) {
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
	priorityOptions = []components.Option{{Value: "Normal", Label: "Normal"}, {Value: "Urgent", Label: "Urgent"}}
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
			{Key: "date", Label: "Order date", Priority: components.PMeta},
			{Key: "hindi", Label: "Name in Hindi", Priority: components.PLow},
		},
		Empty: components.EmptyState("No sample orders yet.", &components.Link{Label: "New sample order", Href: "/_gallery/wizard-1"}),
	}
	if empty {
		return p
	}
	values := map[string]string{"number": uitest.LongCode60, "amount": uitest.MaxMoney, "date": "24-10-2026", "hindi": uitest.LongDevanagari}
	p.Rows = []components.Row{
		{Href: "/_gallery/datalist", Party: uitest.LongName100, Values: values, State: doctype.StatePending},
		{Href: "/_gallery/datalist", Values: values, State: doctype.StateApproved},
		{Href: "/_gallery/datalist", Party: uitest.LongDevanagari, Values: map[string]string{"number": "Draft 42", "amount": "₹0.00"}, State: doctype.StateDraft},
	}
	return p
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
	return out
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

// searchURL is the picker search for the listbox with id list.
func searchURL(list string) string { return "/_gallery/items?list=" + list }

var attachments = []components.Attachment{
	{ID: 1, Kind: "image", Name: uitest.LongName100 + ".jpg", Size: "2.4 MB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:30", URL: brand.AppLogoURL, RemoveURL: "/_gallery/upload"},
	{ID: 2, Kind: "heic", Name: "IMG_0042.HEIC", Size: "3.1 MB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:31", URL: "/_gallery/upload", RemoveURL: "/_gallery/upload"},
	{ID: 3, Kind: "pdf", Name: uitest.LongCode60 + ".pdf", Size: "180 KB", UploadedBy: uitest.LongName100, UploadedAt: "24-10-2026 10:32", URL: "/_gallery/upload"},
}
