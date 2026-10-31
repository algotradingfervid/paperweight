package print_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/types"
)

var grnInfo = doctype.Info{DocType: testGRN, ID: 1, State: doctype.StateApproved, Number: "GR/26-27/00001", Display: "GR/26-27/00001"}

// grnTransport is the reference GRN with every MOD-GRN addition filled and
// a second line carrying a reject reason.
func grnTransport() print.GRNDoc {
	d := referenceGRN()
	d.ChallanLabel = "Delivery challan no."
	d.VehicleNo = "MH12AB1234"
	d.Transporter = "Sai Roadways"
	d.EWayBillNo = "331009876543"
	d.Remarks = "Unloaded at gate 2."
	d.ReverseChargeNote = "Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor."
	d.Lines[0].RejectReason = "Bent angles"
	d.Lines = append(d.Lines, print.GRNLine{Sr: 2, Item: "MS-PLATE-6 MS Plate 6mm", HSN: "7208", UoM: "kg", Received: 500_000, Accepted: 500_000})
	return d
}

// GRN spec §9: the transport fields, remarks and a line's reject reason
// print when set; CD-272 Gap 5: the challan label comes from the caller.
func TestPrint_GRNTransportFields(t *testing.T) {
	d := grnTransport()
	html := renderView(t, grnInfo, print.GRNDocument(d))
	printtest.Golden(t, "grn_transport_fields.golden.html", []byte(html))

	doc := pageDoc(t, html)
	var challan []string
	doc.Find(".challan").Each(func(_ int, s *goquery.Selection) { challan = append(challan, s.Text()) })
	wantChallan := []string{"Delivery challan no.: SST/1182", "Challan Date: 23-10-2026"}
	if strings.Join(challan, "|") != strings.Join(wantChallan, "|") {
		t.Errorf("challan lines = %q, want %q", challan, wantChallan)
	}
	for sel, want := range map[string]string{
		".vehicle":        "Vehicle no.: MH12AB1234",
		".transporter":    "Transporter: Sai Roadways",
		".eway-bill":      "E-way bill no.: 331009876543",
		".remarks":        "Remarks: Unloaded at gate 2.",
		".reverse-charge": d.ReverseChargeNote,
	} {
		if got := doc.Find(sel).Text(); got != want {
			t.Errorf("%s = %q, want %q", sel, got, want)
		}
	}
	reasons := doc.Find("td.item .reject-reason")
	if reasons.Length() != 1 {
		t.Fatalf("reject reasons = %d, want 1 (only the first line has one)", reasons.Length())
	}
	if got := reasons.Text(); got != "Reason for rejecting: Bent angles" {
		t.Errorf("reject reason = %q", got)
	}
	if got := doc.Find("tbody tr").First().Find("td.item .reject-reason").Length(); got != 1 {
		t.Errorf("reject reason not under the first line's item")
	}
}

// FILE-064: each addition is left out, with no empty wrapper, when empty;
// a blank challan label prints the neutral label.
func TestPrint_GRNAdditionsOmittedWhenEmpty(t *testing.T) {
	d := referenceGRN()
	doc := pageDoc(t, renderView(t, grnInfo, print.GRNDocument(d)))
	for _, sel := range []string{".vehicle", ".transporter", ".eway-bill", ".remarks", ".reverse-charge", ".reject-reason"} {
		if n := doc.Find(sel).Length(); n != 0 {
			t.Errorf("%s rendered %d times when empty", sel, n)
		}
	}
	if got := doc.Find(".challan").First().Text(); got != "Challan or invoice no.: SST/1182" {
		t.Errorf("blank label challan line = %q", got)
	}
}

// GRN spec §9: a service GRN with no challan prints a dash and no challan
// date line.
func TestPrint_GRNBlankChallanDash(t *testing.T) {
	d := referenceGRN()
	d.ChallanNo = ""
	d.ChallanDate = types.Date{}
	d.Lines = []print.GRNLine{{Sr: 1, Item: "SVC-INSTALL Installation", HSN: "9954", UoM: "job", Received: 1_000, Accepted: 1_000}}
	html := renderView(t, grnInfo, print.GRNDocument(d))
	doc := pageDoc(t, html)
	var challan []string
	doc.Find(".challan").Each(func(_ int, s *goquery.Selection) { challan = append(challan, s.Text()) })
	if want := "Challan or invoice no.: -"; len(challan) != 1 || challan[0] != want {
		t.Errorf("challan lines = %q, want only %q", challan, want)
	}
	if strings.Contains(html, "Challan Date") || strings.Contains(html, "00-00-0000") {
		t.Errorf("print has a challan date line for a zero date")
	}
}

// GRN spec §7: the GRN print copies the PO's reverse-charge note.
func TestPrint_GRNReverseChargeNote(t *testing.T) {
	d := referenceGRN()
	d.ReverseChargeNote = "Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor."
	doc := pageDoc(t, renderView(t, grnInfo, print.GRNDocument(d)))
	if got := doc.Find(".reverse-charge").Text(); got != d.ReverseChargeNote {
		t.Errorf("reverse-charge note = %q, want %q", got, d.ReverseChargeNote)
	}
}
