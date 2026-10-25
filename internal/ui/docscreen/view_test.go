package docscreen_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestDocView_FromRecord(t *testing.T) {
	e := newDocEnv(t, allRights...)
	info := sampleInfo(42, doctype.StateApproved)
	docscreentest.Seed(e.docs, info, doc.Record{
		Version: 3,
		Values:  map[string]string{"supplier_id": "7", "order_date": "2026-10-24", "amount": "94164.00"},
		Lines:   []map[string]string{{"id": "1", "item_id": "3", "qty": "1200.000", "uom": "kg", "rate": "62.5000"}},
	})
	approvalView := docscreen.ApprovalView{Needed: true, History: []docscreen.HistoryRow{{At: "24-10-2026 10:30", By: "Ravi Kulkarni", Action: "Approved"}}}
	origin := docscreen.Origin{AmendedFrom: "SO/26-27/00000", AmendedFromID: 40}
	audit := []docscreen.HistoryRow{{At: "24-10-2026 09:00", By: "Priya Shah", Action: "Saved"}}
	files := []components.Attachment{{ID: 1, Name: "invoice.pdf", Kind: "pdf", URL: "/files/1"}}
	e.extras.SetApproval(docscreentest.Sample, 42, approvalView)
	e.extras.SetOrigin(docscreentest.Sample, 42, origin)
	e.extras.SetAudit(docscreentest.Sample, 42, audit)
	e.extras.SetAttachments(docscreentest.Sample, 42, files)

	v, err := e.d.View(context.Background(), priya, docscreentest.Sample, 42)
	if err != nil {
		t.Fatal(err)
	}
	if n := e.db.Calls(); n != 1 {
		t.Errorf("Read called %d times, want one transaction for the record", n)
	}
	if v.Info.Display != "SO/26-27/00001" || v.Version != 3 {
		t.Errorf("Info.Display %q, Version %d", v.Info.Display, v.Version)
	}
	wantRef := auth.RecordRef{DocType: docscreentest.Sample, ID: 42, CreatedBy: priya.ID, EmployeeID: 5, ProjectIDs: []int64{9}}
	if !reflect.DeepEqual(v.Record, wantRef) {
		t.Errorf("Record = %+v, want %+v", v.Record, wantRef)
	}

	type lv struct{ Label, Value string }
	var header []lv
	for _, p := range v.Header {
		header = append(header, lv{p.Label, p.Value})
	}
	wantHeader := []lv{{"Supplier", "SHREE Shree Steel Traders"}, {"Order date", "24-10-2026"}, {"Amount", "₹94,164.00"}}
	if !slices.Equal(header, wantHeader) {
		t.Errorf("Header = %q, want %q (every non-empty header field, CD-68)", header, wantHeader)
	}
	for k, want := range map[string]string{"supplier_id": "7", "order_date": "24-10-2026", "amount": "94,164.00"} {
		if got := v.Inputs[k]; got != want {
			t.Errorf("Inputs[%s] = %q, want %q", k, got, want)
		}
	}

	if v.GrandTotal != "₹94,164.00" {
		t.Errorf("GrandTotal = %q", v.GrandTotal)
	}
	m, _ := types.ParseMoney("94164.00")
	words, werr := print.InWordsINR(m)
	if werr != nil {
		words = "" // print.InWordsINR is a WP0 stub until FILE-PRINT merges
	}
	if v.InWords != words {
		t.Errorf("InWords = %q, want print.InWordsINR's %q", v.InWords, words)
	}

	if len(v.Lines) != 1 {
		t.Fatalf("Lines = %d, want 1", len(v.Lines))
	}
	l := v.Lines[0]
	if l.No != 1 || l.Card.No != 1 {
		t.Errorf("line No = %d, card No = %d", l.No, l.Card.No)
	}
	if l.Card.QtyRate != "1,200 kg × ₹62.50" {
		t.Errorf("card = %q, want \"1,200 kg × ₹62.50\" (CD-61)", l.Card.QtyRate)
	}
	if l.Card.Item != "MS Angle 50x50x5" || l.Card.Code != "MS-ANGLE" {
		t.Errorf("card item %q code %q, want the Item master's name and code", l.Card.Item, l.Card.Code)
	}
	for k, want := range map[string]string{"item_id": "3", "qty": "1,200", "rate": "62.50"} {
		if got := l.Inputs[k]; got != want {
			t.Errorf("line Inputs[%s] = %q, want %q", k, got, want)
		}
	}
	if len(v.Problems) != 0 {
		t.Errorf("Problems = %+v, want none", v.Problems)
	}

	if !reflect.DeepEqual(v.Approval, approvalView) || !reflect.DeepEqual(v.Origin, origin) ||
		!reflect.DeepEqual(v.History, audit) || !reflect.DeepEqual(v.Attachments, files) {
		t.Errorf("Extras not carried: approval %+v origin %+v history %+v attachments %+v", v.Approval, v.Origin, v.History, v.Attachments)
	}

	// Required header and line fields left empty are Problems.
	docscreentest.Seed(e.docs, sampleInfo(44, doctype.StateDraft), doc.Record{
		Version: 1,
		Values:  map[string]string{"supplier_id": "7", "order_date": ""},
		Lines:   []map[string]string{{"id": "5", "item_id": "3", "qty": ""}},
	})
	v, err = e.d.View(context.Background(), priya, docscreentest.Sample, 44)
	if err != nil {
		t.Fatal(err)
	}
	type fl struct {
		Field string
		Line  int
	}
	var got []fl
	for _, p := range v.Problems {
		got = append(got, fl{p.Field, p.Line})
	}
	slices.SortFunc(got, func(a, b fl) int { return strings.Compare(a.Field, b.Field) })
	if want := []fl{{"order_date", 0}, {"qty", 1}}; !slices.Equal(got, want) {
		t.Errorf("Problems = %+v, want %+v", got, want)
	}
}

func TestHistory_EscapesValues(t *testing.T) {
	rows := []docscreen.HistoryRow{
		{At: "24-10-2026 14:45", By: "Ravi Kulkarni", Action: "Approved"},
		{At: "24-10-2026 10:30", By: "Priya Shah", Action: "Saved", Changes: []docscreen.ChangeRow{
			{Field: "Remarks", Before: "", After: "<script>alert(1)</script>"},
			{Field: "Supplier", Before: `<img src=x onerror="alert(2)">`, After: "SHREE Shree Steel Traders"},
		}},
	}
	doc := uitest.Render(t, docscreen.History(rows))
	if doc.Find("script, img").Length() != 0 {
		t.Fatal("user text became markup")
	}
	text := norm(doc.Text())
	for _, want := range []string{
		"24-10-2026 14:45 · Ravi Kulkarni · Approved",
		"24-10-2026 10:30 · Priya Shah · Saved",
		"Remarks: (empty) → <script>alert(1)</script>",
		`Supplier: <img src=x onerror="alert(2)"> → SHREE Shree Steel Traders`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("history lacks %q; text %q", want, text)
		}
	}
	if strings.Index(text, "14:45") > strings.Index(text, "10:30") {
		t.Error("rows are not in the order given (newest first)")
	}
}
