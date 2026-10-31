package print_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
)

// CD-285: the challan date line's label comes from the caller, as the
// challan number's does (CD-272 Gap 5); blank keeps "Challan Date". Each
// label has its golden; the blank one is grn_approved.
func TestPrint_GRNChallanDateLabel(t *testing.T) {
	for _, tc := range []struct {
		name, label, dateLabel, golden string
		want                           []string
	}{
		{"delivery challan", "Delivery challan no.", "Challan date", "grn_label_challan.golden.html",
			[]string{"Delivery challan no.: SST/1182", "Challan date: 23-10-2026"}},
		{"tax invoice", "Tax invoice no.", "Invoice date", "grn_label_invoice.golden.html",
			[]string{"Tax invoice no.: SST/1182", "Invoice date: 23-10-2026"}},
		{"blank", "", "", "grn_approved.golden.html",
			[]string{"Challan or invoice no.: SST/1182", "Challan Date: 23-10-2026"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := referenceGRN()
			d.ChallanLabel, d.ChallanDateLabel = tc.label, tc.dateLabel
			html := renderView(t, grnInfo, print.GRNDocument(d))
			printtest.Golden(t, tc.golden, []byte(html))
			var got []string
			pageDoc(t, html).Find(".challan").Each(func(_ int, s *goquery.Selection) { got = append(got, s.Text()) })
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("challan lines = %q, want %q", got, tc.want)
			}
		})
	}
}
