package goodsreceipt_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-GRN G9 review rows: the reports' row order with ids that don't follow
// the receipt dates, the photo count's DocType, the fail-closed read scope
// and the PO number a viewer can't read (CD-233, CD-340). On the real
// auth.Service (grntest).

// Bills to enter runs oldest receipt first, then by GRN id (UX §5.11;
// g9_reports.sql); Received items newest receipt first, then GRN id
// descending, then each GRN's lines by line_no (UX §5.12). The early GRN is
// made after every other one with receipt date 20-10-2026, so its id is the
// largest and its date the oldest; G1, G2 and GC share 24-10-2026 with
// rising ids; the early GRN's line_no order is the reverse of its line ids.
func TestGRN_ReportsOrder(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	early := env.EarlyReceipt(t)
	if early.ID <= w.G3.ID {
		t.Fatalf("early GRN id %d isn't after G3's %d; the order rows test nothing", early.ID, w.G3.ID)
	}

	var bills []int64
	for _, r := range g9Bills(t, env, env.Suresh, goodsreceipt.BillsFilter{}) {
		bills = append(bills, r.ID)
	}
	if want := []int64{early.ID, w.G1.ID, w.G2.ID, w.GC.ID, w.G3.ID}; !slices.Equal(bills, want) {
		t.Errorf("Bills to enter order = %v, want %v (oldest receipt first: early 20-10; G1, G2, GC on 24-10 by id; G3 01-11)", bills, want)
	}

	var items []int64
	for _, r := range g9Items(t, env, env.Suresh, goodsreceipt.ItemsFilter{}) {
		items = append(items, r.LineID)
	}
	want := []int64{
		w.G3.Lines[0].ID,
		w.GC.Lines[0].ID, w.G2.Lines[0].ID, w.G1.Lines[0].ID, w.G1.Lines[1].ID,
		early.Lines[1].ID, early.Lines[0].ID, // line_no 1 (WIRE-18G), then line_no 2
	}
	if !slices.Equal(items, want) {
		t.Errorf("Received items order = %v, want %v (newest receipt first: G3 01-11; GC, G2, G1 on 24-10 by id descending, G1's lines by line_no; early 20-10, its lines by line_no)", items, want)
	}
}

// The photo count counts the GRN's own attachments only: an attachment on a
// purchase order whose id is G2's (a copy of G1's photo row with plain SQL,
// since files.Attach needs a PO with that id) leaves G2 at 0 photos.
func TestGRN_BillsReportPhotosOwnDocType(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	res, err := env.DB.SQL().ExecContext(t.Context(),
		`INSERT INTO attachment (parent_doctype, parent_id, file_name, size, content_type, sha256, uploaded_by, uploaded_at)
		SELECT ?, ?, file_name, size, content_type, sha256, uploaded_by, uploaded_at FROM attachment
		WHERE parent_doctype = ? AND parent_id = ? LIMIT 1`, purchase.DocType, w.G2.ID, goodsreceipt.DocType, w.G1.ID)
	if err != nil {
		t.Fatalf("copy an attachment onto purchase order %d: %v", w.G2.ID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("copied %d attachment rows, want 1", n)
	}
	got := map[int64]int{}
	for _, r := range g9Bills(t, env, env.Admin, goodsreceipt.BillsFilter{}) {
		got[r.ID] = r.Photos
	}
	if got[w.G2.ID] != 0 {
		t.Errorf("G2 photos = %d, want 0 (the attachment is on %s %d, not the GRN)", got[w.G2.ID], purchase.DocType, w.G2.ID)
	}
	if got[w.G1.ID] != 2 {
		t.Errorf("G1 photos = %d, want 2", got[w.G1.ID])
	}
}

// A viewer with no right on the Goods Receipt is refused by both reports
// with AUTH's read refusal and gets no rows: Auth.Scope's refusal is never
// read as "every row".
func TestGRN_ReportsNoReadRight(t *testing.T) {
	env := grntest.New(t)
	env.NewReportWorld(t)
	p := env.NoReceiptRights(t)

	bills, err := env.Service.BillsReport(t.Context(), p, goodsreceipt.BillsFilter{})
	var de *auth.DeniedError
	if !errors.As(err, &de) || de.Right != auth.RightRead {
		t.Errorf("BillsReport as %s err = %v, want AUTH's read refusal (*auth.DeniedError)", p.Name, err)
	}
	if len(bills) != 0 {
		t.Errorf("BillsReport as %s = %d rows, want none", p.Name, len(bills))
	}

	items, err := env.Service.ItemsReport(t.Context(), p, goodsreceipt.ItemsFilter{})
	de = nil
	if !errors.As(err, &de) || de.Right != auth.RightRead {
		t.Errorf("ItemsReport as %s err = %v, want AUTH's read refusal (*auth.DeniedError)", p.Name, err)
	}
	if len(items) != 0 {
		t.Errorf("ItemsReport as %s = %d rows, want none", p.Name, len(items))
	}
}

// CD-340 (CD-233, as CD-297 for the register CSV): a report row's PO no. is
// blank for a viewer who can't read that purchase order and the snapshot
// number for one who can. The Receipt Reader reads every GRN and no PO; the
// PO Own Reader reads every GRN and only BharatPO (its creator); Priya reads
// the METRO POs.
func TestGRN_ReportsHideUnreadablePONumber(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	reader := env.ReceiptReader(t)
	own := env.POOwnReader(t, w.BharatPO.ID)

	for _, tc := range []struct {
		who  string
		p    auth.Principal
		want map[int64]string // GRN id → PO no.
	}{
		{"Receipt Reader (no PO)", reader, map[int64]string{w.G1.ID: "", w.G2.ID: "", w.GT.ID: ""}},
		{"PO Own Reader (BharatPO only)", own, map[int64]string{w.G1.ID: "", w.G2.ID: w.BharatPO.Number, w.GT.ID: ""}},
		{"Priya (METRO POs)", env.Priya, map[int64]string{w.G1.ID: w.RefPO.Number, w.G2.ID: w.BharatPO.Number}},
	} {
		bills := map[int64]string{}
		for _, r := range g9Bills(t, env, tc.p, goodsreceipt.BillsFilter{}) {
			bills[r.ID] = r.PONumber
		}
		items := map[int64]string{}
		for _, r := range g9Items(t, env, tc.p, goodsreceipt.ItemsFilter{}) {
			items[r.GRNID] = r.PONumber
		}
		for id, want := range tc.want {
			if got, ok := bills[id]; !ok || got != want {
				t.Errorf("%s: Bills to enter PO no. of GRN %d = %q (row %v), want %q", tc.who, id, got, ok, want)
			}
			if got, ok := items[id]; !ok || got != want {
				t.Errorf("%s: Received items PO no. of GRN %d = %q (row %v), want %q", tc.who, id, got, ok, want)
			}
		}
	}
}
