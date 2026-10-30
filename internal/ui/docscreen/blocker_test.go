package docscreen_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// unreadableReceipts is an AUTH that can't answer a read on receipts: not a
// refusal, a failed check.
type unreadableReceipts struct {
	auth.Service
	err error
}

func (a unreadableReceipts) CanTx(ctx context.Context, tx *db.Tx, p auth.Principal, dt string, r auth.Right, rec *auth.RecordRef) error {
	if dt == fixture.Receipt && r == auth.RightRead {
		return a.err
	}
	return a.Service.CanTx(ctx, tx, p, dt, r, rec)
}

// cancelBlocked posts a cancel of an order a receipt blocks, as the admin
// under an AUTH whose read on receipts answers err, and returns the refusal
// and what was logged.
func cancelBlocked(t *testing.T, err error) (*goquery.Selection, string) {
	t.Helper()
	h := fixturetest.New(t)
	ctx := context.Background()
	order := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000", Rate: "62.5000"})
	var line int64
	if err := h.DB.SQL().QueryRowContext(ctx, `SELECT id FROM fixture_order_line WHERE parent_id = ?`, order.ID).Scan(&line); err != nil {
		t.Fatal(err)
	}
	receipt := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: line, Qty: "400.000"})
	h.MustDo(t, ctx, h.Suresh, fixture.Receipt, receipt.ID, doc.ActionSubmit, doc.ActionInput{Version: receipt.Version})

	var logged bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	e := scopeMount(t, h, h.Engine, unreadableReceipts{Service: h.Auth, err: err}, h.Admin)
	var version int64
	if err := h.DB.SQL().QueryRowContext(ctx, `SELECT version FROM fixture_order WHERE id = ?`, order.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	rec := e.post(t, "/doc/"+fixture.Order+"/"+itoa(order.ID)+"/cancel", url.Values{"version": {itoa(version)}, "reason": {"Vendor changed the terms"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST cancel = %d, want 409: %.300s", rec.Code, rec.Body)
	}
	alert := uitest.Doc(t, rec).Find(`[role="alert"]`)
	if !strings.Contains(alert.Text(), "is linked to it") {
		t.Errorf("the refusal is gone: %q", alert.Text())
	}
	return alert, logged.String()
}

// TestReasonPage_FailedBlockerReadIsLoggedAndHidesTheLink: the refusal stands
// alone, so a failed check loses only the link, and the failure is logged.
func TestReasonPage_FailedBlockerReadIsLoggedAndHidesTheLink(t *testing.T) {
	alert, logged := cancelBlocked(t, errors.New("rights table gone"))
	if alert.Find("a").Length() != 0 {
		t.Error("a failed read check still shows the link")
	}
	if !strings.Contains(logged, "rights table gone") {
		t.Errorf("the failed check isn't logged: %q", logged)
	}
}

// TestReasonPage_HiddenBlockerIsNoLinkAndNoError: a blocker the check answers
// not found for is hidden like a refused one, not logged as a failure (CD-248).
func TestReasonPage_HiddenBlockerIsNoLinkAndNoError(t *testing.T) {
	alert, logged := cancelBlocked(t, auth.ErrNotFound)
	if alert.Find("a").Length() != 0 {
		t.Error("a blocker the user can't find still shows the link")
	}
	if strings.Contains(logged, "blocker link") {
		t.Errorf("a not-found answer is logged as a failed check: %q", logged)
	}
}
