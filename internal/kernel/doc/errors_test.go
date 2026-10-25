package doc_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Texts the other refusal cases state (DOC-001, DOC-005, DOC-069).
const (
	lkMsgSupplierRequired  = "Supplier is required."
	lkMsgOnlyApprovedClose = "Only approved documents can be closed."
	lkMsgGone              = "This document no longer exists. It may have been deleted."
	lkMsgStale             = "Someone else changed %s after you opened it. Reload it and make your changes again."
)

// lkHTTP fails unless err is a doctype.HTTPError with status and text msg.
func lkHTTP(t *testing.T, err error, status int, msg string) doctype.HTTPError {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v (%T), want a doctype.HTTPError", err, err)
	}
	if he.Status() != status {
		t.Errorf("Status() = %d, want %d (err %v)", he.Status(), status, err)
	}
	if he.Error() != msg {
		t.Errorf("Error() = %q, want %q", he.Error(), msg)
	}
	return he
}

// DOC-103
func TestDoc_Errors_HTTPStatus(t *testing.T) {
	t.Run("missing required field is 422 with field errors", func(t *testing.T) {
		h := fixturetest.New(t)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
			"project_id": lkID(h.M.Project["PRJ-METRO"]), "order_date": "2026-10-24", "amount": "0",
		}})
		he := lkHTTP(t, err, 422, lkMsgFixFields)
		fes := he.FieldErrors()
		if len(fes) == 0 {
			t.Fatal("FieldErrors() is empty, want the supplier_id error")
		}
		found := false
		for _, fe := range fes {
			if fe.Path() == "supplier_id" && fe.Message == lkMsgSupplierRequired {
				found = true
			}
		}
		if !found {
			t.Errorf("FieldErrors() = %#v, want one with Path %q and Message %q", fes, "supplier_id", lkMsgSupplierRequired)
		}
	})

	t.Run("guard refusal is 409 without field errors", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionClose,
			doc.ActionInput{Version: res.Version, Reason: lkCloseReason})
		he := lkHTTP(t, err, 409, lkMsgOnlyApprovedClose)
		if fes := he.FieldErrors(); fes != nil {
			t.Errorf("FieldErrors() = %#v, want nil", fes)
		}
	})

	t.Run("stale version is 409 without field errors", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		in := h.In(h.Load(t, fixture.Order, res.ID))
		in.Version = res.Version - 1
		in.Values["remarks"] = "Deliver to Pune site"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSave, in)
		he := lkHTTP(t, err, 409, fmt.Sprintf(lkMsgStale, lkDraft(res.ID)))
		if fes := he.FieldErrors(); fes != nil {
			t.Errorf("FieldErrors() = %#v, want nil", fes)
		}
	})

	t.Run("blocked cancel is 409 without field errors", func(t *testing.T) {
		h, o, _ := lkBlockedSetup(t)
		he := lkHTTP(t, lkCancel(t, h, o), 409, fmt.Sprintf(lkMsgBlockedOne, lkPO, lkGR, "approved", lkGR))
		if fes := he.FieldErrors(); fes != nil {
			t.Errorf("FieldErrors() = %#v, want nil", fes)
		}
	})

	t.Run("missing id is 404 and ErrNotFound", func(t *testing.T) {
		h := fixturetest.New(t)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 9999, doc.ActionSubmit, doc.ActionInput{Version: 1})
		lkHTTP(t, err, 404, lkMsgGone)
		if !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("Do: errors.Is(%v, doctype.ErrNotFound) = false, want true", err)
		}
		err = h.DB.Read(t.Context(), func(tx *db.Tx) error {
			_, err := h.Engine.Load(t.Context(), tx, fixture.Order, 9999)
			return err
		})
		lkHTTP(t, err, 404, lkMsgGone)
		if !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("Load: errors.Is(%v, doctype.ErrNotFound) = false, want true", err)
		}
	})

	t.Run("busy is 503", func(t *testing.T) {
		if got := (&doc.Error{Kind: doc.KindBusy}).Status(); got != 503 {
			t.Errorf("KindBusy Status() = %d, want 503", got)
		}
	})

	t.Run("permission refusal is AUTH's own error", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		h.Auth.Deny = func(_ auth.Principal, _ string, r auth.Right) bool { return r == "submit" }
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		if !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("err = %v, want auth.ErrForbidden", err)
		}
		if err != auth.ErrForbidden { // CD-44: passed through unchanged, not wrapped
			t.Errorf("err = %#v, want AUTH's value itself", err)
		}
		var de *doc.Error
		if errors.As(err, &de) {
			t.Errorf("err is a *doc.Error (%v), want AUTH's error passed through", de)
		}
		lkHTTP(t, err, 403, auth.ErrForbidden.Error())
	})
}
