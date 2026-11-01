package purchaseweb

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// e5Header is the PO page's header card: its key facts (label=value) and
// its steps (text|aria-current).
func e5Header(p p15dPage) (facts, steps []string) {
	p.doc.Find("main#main .doc-head dl.doc-head__facts > div").Each(func(_ int, s *goquery.Selection) {
		facts = append(facts, p15dCollapse(s.Find("dt").Text())+"="+p15dCollapse(s.Find("dd").Text()))
	})
	p.doc.Find("main#main .doc-head ol.lifecycle > li").Each(func(_ int, s *goquery.Selection) {
		steps = append(steps, p15dCollapse(s.Text())+"|"+s.AttrOr("aria-current", ""))
	})
	return facts, steps
}

// UI v2 E5 (§5.13 items 3 and 4, A-26): a PO's header shows Vendor, Deliver
// to, Order date and Required by, and its steps mark the state, with the
// word Rejected on a rejected PO. Required by is left out when blank.
func TestPO_HeaderKeyFactsAndSteps(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	today := types.FormatDate(types.Today(env.Clock))
	vendor, site := "SHREE Shree Steel Traders", "METRO-SITE METRO-SITE" // the Details values (code and name)
	for _, c := range []struct {
		s     doctype.State
		reqBy string
		facts []string
		steps []string
	}{
		{doctype.StatePending, "07-11-2026", []string{"Vendor=" + vendor, "Deliver to=" + site, "Order date=" + today, "Required by=07-11-2026"},
			[]string{"Draft (done)|", "Pending approval|step", "Approved|"}},
		{doctype.StateRejected, "", []string{"Vendor=" + vendor, "Deliver to=" + site, "Order date=" + today},
			[]string{"Draft (done)|", "Rejected|step", "Approved|"}},
		{doctype.StateApproved, "", []string{"Vendor=" + vendor, "Deliver to=" + site, "Order date=" + today},
			[]string{"Draft (done)|", "Pending approval (done)|", "Approved|step"}},
		{doctype.StateClosed, "", []string{"Vendor=" + vendor, "Deliver to=" + site, "Order date=" + today},
			[]string{"Draft (done)|", "Pending approval (done)|", "Approved (done)|", "Closed|step"}},
	} {
		t.Run(string(c.s), func(t *testing.T) {
			var opts []purchasetest.OrderOpt
			if c.reqBy != "" {
				opts = append(opts, purchasetest.OrderHeader(map[string]string{"required_by": c.reqBy}))
			}
			po := w.order(t, c.s, opts...)
			facts, steps := e5Header(w.page(t, env.Suresh, po.ID))
			if !slices.Equal(facts, c.facts) {
				t.Errorf("facts = %q, want %q", facts, c.facts)
			}
			if !slices.Equal(steps, c.steps) {
				t.Errorf("steps = %q, want %q", steps, c.steps)
			}
		})
	}
}
