package app_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// istNow is the fixed clock in FormatIST form (00-fixtures.md).
const istNow = "24-10-2026 10:30"

// listIDs are the rows list.Build returns under p's read scope.
func (w *world) listIDs(p auth.Principal, dt *doctype.DocType) []int64 {
	w.t.Helper()
	ctx := context.Background()
	scope, err := w.app.Auth().Scope(ctx, p, dt.Name, "read")
	if err != nil {
		w.t.Fatalf("Scope: %v", err)
	}
	q, args, err := list.Build(dt, list.Query{Filters: list.DefaultFilters(dt), Page: 1, PageSize: 25}, scope)
	if err != nil {
		w.t.Fatalf("list.Build: %v", err)
	}
	var ids []int64
	err = w.app.DB().Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, err := list.ScanRow(dt, rows)
			if err != nil {
				return err
			}
			ids = append(ids, r.ID)
		}
		return rows.Err()
	})
	if err != nil {
		w.t.Fatalf("read the list: %v", err)
	}
	return ids
}

var orderHref = regexp.MustCompile(`^` + regexp.QuoteMeta(orderPath) + `/([0-9]+)$`)

// CD-78, CD-91. The adapters have no exported names, so each one is reached
// through the pages that read it and compared with the owning service.
func TestApp_UIAdaptersMatchServices(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	ctx := context.Background()
	admin := w.signedIn(adminEmail)
	priya := w.signedIn(priyaEmail)
	ravi := w.signedIn(raviEmail)

	// An approved order with one attachment, cancelled and amended.
	id := w.pendingOrderWithAttachment(admin, priya, m)
	ravi.act(id, "approve", nil)
	if got := w.state(id); got != "approved" {
		t.Fatalf("order state after approve = %q, want approved", got)
	}
	number := w.text(`SELECT number FROM fixture_order WHERE id = ?`, id)
	if number != "PO/26-27/00001" {
		t.Fatalf("number = %q, want PO/26-27/00001", number)
	}
	priyaP := w.principal(priyaID)
	ravi.act(id, "cancel", url.Values{"reason": {"Wrong supplier"}})
	ravi.act(id, "amend", nil)
	amended := w.text(`SELECT CAST(id AS TEXT) FROM fixture_order WHERE amended_from_id = ?`, id)

	// An order outside Priya's project restriction.
	hidden := admin.newOrder(m, "PRJ-TECHPARK", "1000.00")

	t.Run("lister", func(t *testing.T) {
		dt, ok := doctype.Lookup(fixture.Order)
		if !ok {
			t.Fatal("fixture.Order isn't registered")
		}
		want := w.listIDs(priyaP, dt)
		if len(want) != 2 || slices.Contains(want, hidden) {
			t.Fatalf("list.Build rows for Priya = %v, want the order and its amendment, not %d", want, hidden)
		}
		r := priya.get(orderPath)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d (%q), want 200", orderPath, r.status, r.pageText())
		}
		var got []int64
		r.doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			if m := orderHref.FindStringSubmatch(href); m != nil {
				n, _ := strconv.ParseInt(m[1], 10, 64)
				if !slices.Contains(got, n) {
					got = append(got, n)
				}
			}
		})
		if !slices.Equal(got, want) {
			t.Errorf("rows on the list page = %v, want list.Build's %v", got, want)
		}
		if !strings.Contains(r.pageText(), "SHREE") {
			t.Errorf("list page %q, want the supplier's display value", r.pageText())
		}
	})

	page := priya.get(orderURL(id))
	if page.status != http.StatusOK {
		t.Fatalf("GET the order = %d (%q), want 200", page.status, page.pageText())
	}
	heads := page.doc.Find(".history-head")

	t.Run("history_rows", func(t *testing.T) {
		audits := w.count(`SELECT count(*) FROM audit_log WHERE doc_type = ? AND doc_id = ?`, fixture.Order, id)
		approvals := w.count(`SELECT count(*) FROM approval_history WHERE doctype = ? AND doc_id = ?`, fixture.Order, id)
		if audits == 0 || approvals == 0 {
			t.Fatalf("audit rows %d, approval rows %d, want both above 0", audits, approvals)
		}
		if heads.Length() != audits+approvals {
			t.Errorf("history rows on the page = %d, want %d audit + %d approval", heads.Length(), audits, approvals)
		}
		heads.Each(func(_ int, s *goquery.Selection) {
			if text := strings.TrimSpace(s.Text()); !strings.HasPrefix(text, types.FormatIST(testkit.Clock().Now())+" · ") {
				t.Errorf("history row %q, want the FormatIST time %q first", text, istNow)
			}
		})
	})

	t.Run("approval_history", func(t *testing.T) {
		name := w.text(`SELECT user_name FROM approval_history WHERE doctype = ? AND doc_id = ? AND action = 'approved'`, fixture.Order, id)
		found := false
		heads.Each(func(_ int, s *goquery.Selection) {
			found = found || strings.HasPrefix(strings.TrimSpace(s.Text()), istNow+" · "+name+" · ")
		})
		if name != "Ravi Kulkarni" || !found {
			t.Errorf("APR's approver %q has no history row on the page", name)
		}
	})

	t.Run("attachments", func(t *testing.T) {
		metas, err := w.app.Files().List(ctx, priyaP, auth.RecordRef{
			DocType: fixture.Order, ID: id, CreatedBy: priyaID, ProjectIDs: []int64{m.Project["PRJ-METRO"]},
		})
		if err != nil || len(metas) != 1 {
			t.Fatalf("files List = %+v, %v, want one", metas, err)
		}
		href := "/files/" + strconv.FormatInt(metas[0].ID, 10)
		if page.doc.Find(`.attachments a[href^="`+href+`"]`).Length() == 0 {
			t.Errorf("the page has no attachment link to %s", href)
		}
		if alt, _ := page.doc.Find(`.attachments img`).First().Attr("alt"); alt != metas[0].Name {
			t.Errorf("attachment name on the page = %q, want %q", alt, metas[0].Name)
		}
	})

	t.Run("origin", func(t *testing.T) {
		// The amendment is a draft, so its page is the check page. Ravi's next
		// full page shows the flash the amend left.
		path := orderPath + "/" + amended + "/edit/check"
		r := ravi.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET the amendment = %d, want 200", r.status)
		}
		if want := "Amending " + number; !strings.Contains(r.pageText(), want) {
			t.Errorf("page %q, want the flash %q", r.pageText(), want)
		}
		link := priya.get(path).doc.Find(`a[href="` + orderURL(id) + `"]`)
		if got := strings.TrimSpace(link.First().Text()); got != "Amended from "+number {
			t.Errorf("origin link = %q, want %q", got, "Amended from "+number)
		}

		// state_reason: a rejected document's banner.
		const reason = "Rate too high"
		rejected := priya.newOrder(m, "PRJ-METRO", "94164.00")
		priya.addLine(m, rejected)
		priya.act(rejected, "submit", nil)
		ravi.act(rejected, "reject", url.Values{"reason": {reason}})
		banner := priya.get(orderURL(rejected)).doc.Find(`.banner`).Text()
		if !strings.Contains(banner, reason) {
			t.Errorf("banner of a rejected document = %q, want the reason %q", banner, reason)
		}
	})
}

// pendingOrderWithAttachment is pendingOrder with a receipt added on the draft.
func (w *world) pendingOrderWithAttachment(admin, priya *browser, m fixture.Masters) int64 {
	w.t.Helper()
	admin.purchaseManagerRule()
	id := priya.newOrder(m, "PRJ-METRO", "94164.00")
	priya.addLine(m, id)
	r := priya.upload("/attachments/"+fixture.Order+"/"+strconv.FormatInt(id, 10), "receipt.jpg", filestest.JPEG(1000), nil)
	if r.status != http.StatusCreated {
		w.t.Fatalf("upload = %d (%q), want 201", r.status, r.body)
	}
	priya.act(id, "submit", nil)
	return id
}
