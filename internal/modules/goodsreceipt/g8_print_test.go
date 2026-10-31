package goodsreceipt_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/platform/brand"
)

// G8 · Print (plan MOD-GRN G8; GRN spec §9; CD-285). Every print is fetched
// through the real print handler (print/web) over the real auth.Service, so
// the print right, the read check and the view goodsreceipt registers on
// print.Default are all on the path. Every GRN is made through Engine.Do, so
// the print reads the snapshots G2's Validate wrote.

// g8Print fetches GRN id's print page as p and stops the test unless it is
// served. The letterhead is masters' real one.
func g8Print(t *testing.T, env *grntest.Env, p auth.Principal, id int64) string {
	t.Helper()
	kit := mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
	h := printweb.Routes(printweb.Config{DB: env.DB, Auth: env.Auth, Info: env.Engine, Letterhead: allmasters.New(kit).Letterhead})
	req := httptest.NewRequest(http.MethodGet, "/print/"+goodsreceipt.DocType+"/"+strconv.FormatInt(id, 10), nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("print GRN %d as %s: status %d, want 200:\n%s", id, p.Name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// g8GoldenBytes is html with the brand's hashed logo URL replaced by a fixed
// placeholder, so a logo change doesn't break the goldens.
func g8GoldenBytes(html string) []byte {
	return []byte(strings.ReplaceAll(html, brand.DocumentLogoURL, "/files/1"))
}

// g8Texts are the texts of the elements sel matches in html.
func g8Texts(t *testing.T, html, sel string) []string {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	doc.Find(sel).Each(func(_ int, s *goquery.Selection) { out = append(out, s.Text()) })
	return out
}

// g8Want fails unless the texts sel matches in html are exactly want.
func g8Want(t *testing.T, what, html, sel string, want ...string) {
	t.Helper()
	if got := g8Texts(t, html, sel); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("%s: %s = %q, want %q", what, sel, got, want)
	}
}

// GRN-030: an approved GRN with vehicle no., transporter, e-way bill no.,
// remarks and a line rejecting 20 kg with its reason matches its golden,
// titled "Goods Receipt Note", on METRO-SITE's PUNE letterhead, dated two
// days after the PO, with the
// vendor block from the snapshots, the PO's number and date, the challan
// with its kind's labels (CD-285), and the reason under the item. A
// service-only GRN with no challan prints "-" and no challan date line.
func TestGRN_PrintGolden(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	// The truck comes two days after the order, so the receipt date and the
	// PO date differ on the print.
	env.SetNow(t, env.Clock.Now().Add(48*time.Hour))
	h := g2Header(t, env, po)
	h["challan_no"], h["challan_date"] = "SST/1182", "2026-10-23"
	h["vehicle_no"], h["transporter"], h["eway_bill_no"] = "MH12AB1234", "Sai Roadways", "331009876543"
	h["remarks"] = "Unloaded at gate 2."
	res := g2MustCreate(t, env, env.Suresh, h,
		g2Line(po.Lines[0], "1200", "20", "Bent angles"), g2Line(po.Lines[1], "50", "", ""))
	g2WantApproved(t, "GRN-030 GRN", env, res.ID)
	number := env.Load(t, goodsreceipt.DocType, res.ID).Number

	html := g8Print(t, env, env.Suresh, res.ID)
	printtest.Golden(t, "grn_module_approved.golden.html", g8GoldenBytes(html))
	const what = "GRN-030"
	g8Want(t, what, html, "h1.doc-title", "Goods Receipt Note")
	g8Want(t, what, html, ".doc-number", number)
	g8Want(t, what, html, ".letterhead .branch", "Pune Works")
	g8Want(t, what, html, ".doc-date", "Date: 26-10-2026")
	g8Want(t, what, html, ".po-ref", "Against PO: PO/26-27/00001 dated 24-10-2026")
	g8Want(t, what, html, ".challan", "Delivery challan no.: SST/1182", "Challan date: 23-10-2026")
	g8Want(t, what, html, ".vehicle", "Vehicle no.: MH12AB1234")
	g8Want(t, what, html, ".transporter", "Transporter: Sai Roadways")
	g8Want(t, what, html, ".eway-bill", "E-way bill no.: 331009876543")
	g8Want(t, what, html, ".remarks", "Remarks: Unloaded at gate 2.")
	g8Want(t, what, html, ".party h3", "Vendor")
	g8Want(t, what, html, ".party-name", "Shree Steel Traders")
	g8Want(t, what, html, ".party .gstin", "GSTIN: 27AABCS1429B1ZU")
	// The snapshot's last line is its GSTIN line, which the block prints once.
	g8Want(t, what, html, ".party .address", "Main Road", "Mumbai - 400001", "Maharashtra (27)")
	g8Want(t, what, html, "td.sr", "1", "2")
	g8Want(t, what, html, "td.item", "MS-ANGLE-50 MS Angle 50x50x5Reason for rejecting: Bent angles", "WIRE-18G Binding Wire 18G")
	g8Want(t, what, html, "td.item .reject-reason", "Reason for rejecting: Bent angles")
	g8Want(t, what, html, "td.hsn", "7216", "7217")
	g8Want(t, what, html, "td.uom", "KGS", "KGS")
	g8Want(t, what, html, "td.qty", "1,200", "1,180", "20", "50", "50", "0")

	svc := g2ServicePO(t, env)
	sh := g2Header(t, env, svc)
	sh["challan_no"], sh["challan_kind"] = "", ""
	s := g2MustCreate(t, env, env.Suresh, sh, g2Line(svc.Lines[0], "8", "", ""))
	g2WantApproved(t, "service GRN", env, s.ID)
	shtml := g8Print(t, env, env.Suresh, s.ID)
	printtest.Golden(t, "grn_module_service.golden.html", g8GoldenBytes(shtml))
	g8Want(t, "service GRN", shtml, ".challan", "Challan or invoice no.: -")
	g8Want(t, "service GRN", shtml, "td.item", "SVC-CRANE Crane hire")
}

// CD-285, CD-175: challan_kind stores the display text, and the print's
// number and date labels follow it; a blank kind keeps the kernel's
// defaults. Drafts print too, so a goods GRN with a blank kind can be shown.
func TestGRN_PrintChallanLabels(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	for _, tc := range []struct {
		kind string
		want []string
	}{
		{"Delivery challan", []string{"Delivery challan no.: SST/1182", "Challan date: 23-10-2026"}},
		{"Tax invoice", []string{"Tax invoice no.: SST/1182", "Invoice date: 23-10-2026"}},
		{"", []string{"Challan or invoice no.: SST/1182", "Challan Date: 23-10-2026"}},
	} {
		h := g2Header(t, env, po)
		h["challan_kind"], h["challan_no"], h["challan_date"] = tc.kind, "SST/1182", "2026-10-23"
		res := g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "", ""))
		if st := env.Load(t, goodsreceipt.DocType, res.ID).State; st != doctype.StateDraft {
			t.Fatalf("kind %q: state %s, want draft", tc.kind, st)
		}
		g8Want(t, "kind "+tc.kind, g8Print(t, env, env.Suresh, res.ID), ".challan", tc.want...)
	}
}

// A vendor with no address snapshot prints no address lines, not an empty
// one. Validate writes the snapshot from the PO on every save, so the test
// blanks the stored column after the save, as a PO vendor with no address
// leaves it.
func TestGRN_PrintNoVendorAddress(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	res := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""))
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET snap_vendor_address = '' WHERE id = ?`, res.ID); err != nil {
		t.Fatal(err)
	}
	html := g8Print(t, env, env.Suresh, res.ID)
	if got := g8Texts(t, html, ".party .address"); len(got) != 0 {
		t.Errorf("vendor address lines = %q, want none", got)
	}
	g8Want(t, "no address", html, ".party .gstin", "GSTIN: 27AABCS1429B1ZU")
}

// The letterhead is the PO's branch (snap_branch_id): a GRN on TECH-SITE
// prints on BLR's, not on the head office's.
func TestGRN_PrintBranchLetterhead(t *testing.T) {
	env := grntest.New(t)
	tech := map[string]string{"site_id": g1MasterID(t, env.DB, "site", "TECH-SITE"), "project_id": g1MasterID(t, env.DB, "project", "PRJ-TECHPARK")}
	po := grntest.POAs(t, env, env.Admin, doctype.StateApproved, tech)
	res := g2MustCreate(t, env, env.Admin, g2Header(t, env, po), g2Line(po.Lines[0], "10", "", ""))
	html := g8Print(t, env, env.Admin, res.ID)
	g8Want(t, "TECH-SITE GRN", html, ".letterhead .branch", "Bengaluru Site Office")
	g8Want(t, "TECH-SITE GRN", html, ".letterhead .gstin", "GSTIN: 29AAFCV2449G1Z3")
}

// An unregistered vendor's address snapshot has no GSTIN line
// (address.DisplayText), so every address line prints. The test blanks the
// stored snapshots after the save, as such a PO vendor leaves them.
func TestGRN_PrintUnregisteredVendor(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	res := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""))
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET snap_vendor_gstin = '',
		snap_vendor_address = 'Main Road' || char(10) || 'Mumbai - 400001' || char(10) || 'Maharashtra (27)' WHERE id = ?`, res.ID); err != nil {
		t.Fatal(err)
	}
	html := g8Print(t, env, env.Suresh, res.ID)
	g8Want(t, "unregistered", html, ".party .address", "Main Road", "Mumbai - 400001", "Maharashtra (27)")
	g8Want(t, "unregistered", html, ".party .gstin", "GSTIN: Unregistered")
}

// Lines print in line_no order, not in the order their rows were inserted;
// a line with no item code prints the name alone, with no leading space.
func TestGRN_PrintLineOrder(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	res := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po),
		g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "50", "", ""))
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE goods_receipt_line SET line_no = 3 - line_no,
		snap_item_code = CASE WHEN snap_item_code = 'WIRE-18G' THEN '' ELSE snap_item_code END WHERE parent_id = ?`, res.ID); err != nil {
		t.Fatal(err)
	}
	html := g8Print(t, env, env.Suresh, res.ID)
	g8Want(t, "line order", html, "td.item", "Binding Wire 18G", "MS-ANGLE-50 MS Angle 50x50x5")
	g8Want(t, "line order", html, "td.sr", "1", "2")
}
