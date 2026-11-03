package partyweb_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// listCell is the text of the value labelled label in the list row linking
// to href, "" when the row draws none.
func listCell(t *testing.T, r webtest.Response, href, label string) string {
	t.Helper()
	row := r.Doc.Find(`ul.datalist-rows a[href="` + href + `"]`)
	if row.Length() != 1 {
		t.Fatalf("%d rows link to %s", row.Length(), href)
	}
	var out string
	row.Find("dl").Each(func(_ int, dl *goquery.Selection) {
		if strings.TrimSpace(dl.Find("dt").Text()) == label {
			dd := dl.Find("dd").Clone()
			dd.Find(".datalist-sep").Remove()
			out = strings.Join(strings.Fields(dd.Text()), " ")
		}
	})
	return out
}

// R5 (shell M6, DS rule 10): a party's row shows its code apart from its
// name, the city and GSTIN of its primary billing address, and its role; a
// party with no billing address (so no GSTIN) shows neither, and a disabled
// party, listed with disabled ones, carries the Disabled badge. Columns from
// 48rem: the row has a header.
func TestScreens_PartyListRowsIdentify(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	tata := w.create(t, party.Input{Code: "C-TATA", Name: "Tata Projects", IsCustomer: true, GSTCategory: "Unregistered"})
	if _, err := w.addresses.Create(context.Background(), w.h.Env.Kavya, kolkata(tata.ID)); err != nil { // shipping only
		t.Fatal(err)
	}
	w.disable(t, "LOCAL")
	shree, local := w.get(t, "SHREE"), w.get(t, "LOCAL")
	var city, gst string
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT city, gstin FROM address WHERE party_id = ? AND is_primary_billing = 1`, shree.ID).Scan(&city, &gst)
	})
	if city == "" || gst == "" {
		t.Fatalf("SHREE's billing address has city %q, GSTIN %q: the fixture changed", city, gst)
	}

	page := b.Get(t, base+"?include_disabled=true")
	webtest.WantStatus(t, page, http.StatusOK)
	row := page.Doc.Find(`ul.datalist-rows a[href="` + recordURL(shree.ID) + `"]`)
	if got := strings.TrimSpace(row.Find(".datalist-title .datalist-code").Text()); got != "SHREE" {
		t.Errorf("SHREE's code span = %q, want the code on its own", got)
	}
	for _, c := range []struct {
		id                int64
		city, gstin, role string
		disabled          bool
	}{
		{shree.ID, city, gst, roleOf(shree), false},
		{tata.ID, "", "", "Customer", false},
		{local.ID, "Pune", "", roleOf(local), true},
	} {
		href := recordURL(c.id)
		if got := listCell(t, page, href, "City"); got != c.city {
			t.Errorf("%s City = %q, want %q", href, got, c.city)
		}
		if got := listCell(t, page, href, "GSTIN"); got != c.gstin {
			t.Errorf("%s GSTIN = %q, want %q", href, got, c.gstin)
		}
		if got := listCell(t, page, href, "Role"); got != c.role {
			t.Errorf("%s Role = %q, want %q", href, got, c.role)
		}
		if got := listCell(t, page, href, "Status"); (got == "Disabled") != c.disabled {
			t.Errorf("%s Status = %q, want disabled %v", href, got, c.disabled)
		}
	}
	var heads []string
	page.Doc.Find(".datalist-head > span").Each(func(_ int, s *goquery.Selection) { heads = append(heads, strings.TrimSpace(s.Text())) })
	if want := "Name City GSTIN Role GST category Status"; strings.Join(heads, " ") != want {
		t.Errorf("header = %q, want %q", heads, want)
	}
}

// The city and GSTIN come from addresses: a reader who may read parties but
// not addresses sees the role only.
func TestScreens_PartyListNoAddressRight(t *testing.T) {
	w := newWorld(t)
	w.grant(t, "Purchase User", map[auth.Right]auth.Scope{}, address.DocType)
	priya := w.h.Env.Priya
	if err := w.h.Env.Kit.Auth.Can(context.Background(), priya, address.DocType, auth.RightRead, nil); err == nil {
		t.Fatal("Priya still reads addresses: the case proves nothing")
	}
	shree := w.get(t, "SHREE")
	page := w.h.Browser(priya).Get(t, base)
	webtest.WantStatus(t, page, http.StatusOK)
	if got := listCell(t, page, recordURL(shree.ID), "City") + listCell(t, page, recordURL(shree.ID), "GSTIN"); got != "" {
		t.Errorf("city and GSTIN = %q for a reader of no address", got)
	}
	if got := listCell(t, page, recordURL(shree.ID), "Role"); got != roleOf(shree) {
		t.Errorf("Role = %q, want %q", got, roleOf(shree))
	}
}

func roleOf(p party.Party) string {
	switch {
	case p.IsVendor && p.IsCustomer:
		return "Vendor and customer"
	case p.IsVendor:
		return "Vendor"
	case p.IsCustomer:
		return "Customer"
	}
	return ""
}
