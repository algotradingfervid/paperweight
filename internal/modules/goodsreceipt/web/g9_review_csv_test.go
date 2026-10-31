package grnweb_test

import (
	"bytes"
	"html"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G9 review rows for the reports' CSVs: CRLF records, a bad filter
// id or month, and the PO no. a viewer can't read (CD-233,
// CD-340). On the real auth.Service (grntest).

// Each CSV record ends with CRLF, as the core export's do (RFC 4180): the
// header line and every data line.
func TestGRNWeb_ReportsCSVCRLF(t *testing.T) {
	env := grntest.New(t)
	env.NewReportWorld(t)
	for _, tc := range []struct {
		path   string
		header []string
	}{{g9wBillsPath, g9wBillsHeader}, {g9wItemsPath, g9wItemsHeader}} {
		rec := g9wGet(t, env, env.Priya, tc.path)
		g9wRecords(t, tc.path, rec, tc.header)
		body := bytes.TrimPrefix(rec.Body.Bytes(), g9wBOM)
		if !bytes.HasPrefix(body, []byte(strings.Join(tc.header, ",")+"\r\n")) {
			t.Errorf("GET %s: header line isn't followed by CRLF: %q", tc.path, body[:min(len(body), 200)])
		}
		if n, crlf := bytes.Count(body, []byte("\n")), bytes.Count(body, []byte("\r\n")); n != crlf || n < 2 {
			t.Errorf("GET %s: %d line feeds, %d of them after CR; want every record ended by CRLF", tc.path, n, crlf)
		}
	}
}

// A filter id that isn't a number, or a month that isn't yyyy-mm, answers as
// the kernel list does and as the expense CSVs do (ruling on G9 review row
// 4; e8_impl_csv_test.go TestE8Impl_BadFilters): list.QueryError's status,
// "Choose a value for {label}." with the filter's label (Vendor: the GRN's
// vendor_id; Item: the PO line's item_id, UX §5.12), or types.ParseDate's
// text for the month, and no CSV, never every row.
func TestGRNWeb_ReportsCSVBadFilter(t *testing.T) {
	env := grntest.New(t)
	env.NewReportWorld(t)
	dateText := func(s string) string {
		_, err := types.ParseDate(s)
		if err == nil {
			t.Fatalf("ParseDate(%q) = nil, want an error", s)
		}
		return err.Error()
	}
	for _, tc := range []struct{ path, want string }{
		{g9wBillsPath + "?vendor=abc", "Choose a value for Vendor."},
		{g9wItemsPath + "?vendor=abc", "Choose a value for Vendor."},
		{g9wItemsPath + "?item=abc", "Choose a value for Item."},
		{g9wItemsPath + "?item=1.5", "Choose a value for Item."},
		{g9wBillsPath + "?month=2026-13", dateText("2026-13")},
		{g9wBillsPath + "?month=2026-10-24", dateText("2026-10-24-01")},
		{g9wItemsPath + "?month=2026-13", dateText("2026-13")},
		{g9wItemsPath + "?month=2026-10-24", dateText("2026-10-24-01")},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := g9wGet(t, env, env.Priya, tc.path)
			body := html.UnescapeString(rec.Body.String())
			if want := (&list.QueryError{}).Status(); rec.Code != want {
				t.Errorf("status = %d, want list's %d", rec.Code, want)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("body = %q, want %q", body[:min(len(body), 200)], tc.want)
			}
			if strings.Contains(body, "GRN no.") || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
				t.Errorf("wrote a CSV: %q", body[:min(len(body), 200)])
			}
		})
	}
}

// CD-340 (CD-233, as CD-297 for the register CSV): in both CSVs the PO no.
// cell is blank for a viewer who can't read that purchase order and the
// snapshot number for one who can. The Receipt Reader reads every GRN and
// no PO; the PO Own Reader every GRN and only BharatPO (its creator); Priya
// the METRO POs. Bills to enter's PO no. is column 5, Received items' 4.
func TestGRNWeb_ReportsCSVHideUnreadablePONumber(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	reader := env.ReceiptReader(t)
	own := env.POOwnReader(t, w.BharatPO.ID)

	for _, tc := range []struct {
		who  string
		p    auth.Principal
		want map[string]string // GRN no. → PO no.
	}{
		{"Receipt Reader (no PO)", reader, map[string]string{w.G1.Number: "", w.G2.Number: "", w.GT.Number: ""}},
		{"PO Own Reader (BharatPO only)", own, map[string]string{w.G1.Number: "", w.G2.Number: w.BharatPO.Number, w.GT.Number: ""}},
		{"Priya (METRO POs)", env.Priya, map[string]string{w.G1.Number: w.RefPO.Number, w.G2.Number: w.BharatPO.Number}},
	} {
		for _, rep := range []struct {
			path   string
			header []string
			col    int
		}{{g9wBillsPath, g9wBillsHeader, 4}, {g9wItemsPath, g9wItemsHeader, 3}} {
			if rep.header[rep.col] != "PO no." {
				t.Fatalf("%s column %d is %q, not PO no.", rep.path, rep.col, rep.header[rep.col])
			}
			got := map[string]string{}
			for _, r := range g9wRecords(t, tc.who+" "+rep.path, g9wGet(t, env, tc.p, rep.path), rep.header) {
				got[r[0]] = r[rep.col]
			}
			for no, want := range tc.want {
				if g, ok := got[no]; !ok || g != want {
					t.Errorf("%s: %s PO no. of %s = %q (row %v), want %q", tc.who, rep.path, no, g, ok, want)
				}
			}
		}
	}
}
