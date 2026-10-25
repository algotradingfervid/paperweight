package filestest

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Setup is what the contract suite needs from the world around a
// files.Service: parents in given states, rights on them, spooled uploads and
// a write transaction. The real Store's Env and the fake's test both supply it.
type Setup interface {
	// Parent creates (or replaces) the parent document info describes and
	// returns its RecordRef.
	Parent(info doctype.Info) auth.RecordRef
	// SetState changes the parent's state.
	SetState(ref auth.RecordRef, state string)
	// Allow gives user u the rights on DocType dt (scope all).
	Allow(u auth.UserID, dt string, rights ...auth.Right)
	// Deny takes the rights on DocType dt away from u.
	Deny(u auth.UserID, dt string, rights ...auth.Right)
	// Hide makes the parent invisible to everyone (a record rule hides it).
	Hide(ref auth.RecordRef)
	// Spool returns b as an upload ready for Attach, with no size limit of its
	// own, so Attach's checks decide.
	Spool(b []byte) *files.Spooled
	// Write runs fn in a write transaction.
	Write(fn func(tx *db.Tx) error) error
	// Now is the service's clock; Advance moves it on.
	Now() time.Time
	Advance(d time.Duration)
}

// Env implements Setup over its real Store, docfake and authfake.
var _ Setup = (*Env)(nil)

// Parent seeds the docfake with info and returns its RecordRef.
func (e *Env) Parent(info doctype.Info) auth.RecordRef {
	e.Docs.SetInfo(info)
	return auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy}
}

// Allow gives u the rights on dt in the authfake.
func (e *Env) Allow(u auth.UserID, dt string, rights ...auth.Right) { e.Auth.Allow(u, dt, rights...) }

// Deny takes the rights on dt away from u in the authfake.
func (e *Env) Deny(u auth.UserID, dt string, rights ...auth.Right) { e.Auth.Deny(u, dt, rights...) }

// Hide hides ref from everyone in the authfake.
func (e *Env) Hide(ref auth.RecordRef) { e.Auth.Hide(ref.DocType, ref.ID) }

// Spool spools b with a limit of len(b), so the size rule is Attach's.
func (e *Env) Spool(b []byte) *files.Spooled {
	e.t.Helper()
	sp, err := e.Store.Spool(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		e.t.Fatalf("filestest.Spool(%d bytes): %v", len(b), err)
	}
	return sp
}

// Write runs fn inside e.DB.Write.
func (e *Env) Write(fn func(tx *db.Tx) error) error { return e.DB.Write(e.t.Context(), fn) }

// Now is e.Clock's time.
func (e *Env) Now() time.Time { return e.Clock.Now() }

// Advance moves e.Clock on by d.
func (e *Env) Advance(d time.Duration) { e.Clock.Advance(d) }

// Contract texts (FILE cases, CD-126).
const (
	contractNotFound   = "File not found."
	contractNoParent   = "Save the document before attaching files."
	contractCancelled  = "This document is cancelled. Attach files to its amended copy instead."
	contractLocked     = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
	contractLimit      = "A document can have at most 20 attachments. Remove one first."
	contractEmpty      = "File is empty. Choose another file."
	contractTooLarge   = "File is larger than 10 MB. Choose a smaller file or compress it."
	contractTypeExe    = "File type .exe is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	contractTypeNone   = "File type (none) is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	contractMismatch   = "File content does not match its .pdf extension. Upload the original file."
	contractAddDenied  = "You don't have permission to add files to PO/26-27/00001."
	contractChangePO   = "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it."
	contractReadPO     = "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it."
	contractDuplicate  = "This file is already attached to Draft 42."
	contractPOType     = "purchase_order"
	contractPOLabel    = "Purchase Order"
	contractPDFMIME    = "application/pdf"
	contractSmallBytes = 4096
)

// contractCase is one behaviour of the suite.
type contractCase struct {
	name string
	run  func(t *testing.T, c *contract)
}

// contract is one case's service, Setup and fixture users.
type contract struct {
	svc                                files.Service
	s                                  Setup
	ravi, meena, priya, deepak, suresh auth.Principal
}

// Run checks the files.Service contract other packages rely on (WP3 Task 1,
// CD-182): permission follows the parent, state rules, count, type and size
// limits, copy on amend, DeleteAll, List order and Open. Every refusal must
// carry the real service's Kind, status and message, so a fake can't accept
// what the real Store refuses (CD-125, CD-163, CD-173). newSvc returns a fresh
// service and its Setup for each case.
func Run(t *testing.T, newSvc func(t *testing.T) (files.Service, Setup)) {
	t.Helper()
	for _, tc := range contractCases() {
		t.Run(tc.name, func(t *testing.T) {
			svc, s := newSvc(t)
			c := &contract{
				svc:    svc,
				s:      s,
				ravi:   testkit.Principal(t, "ravi@vardhan.test"),
				meena:  testkit.Principal(t, "meena@vardhan.test"),
				priya:  testkit.Principal(t, "priya@vardhan.test"),
				deepak: testkit.Principal(t, "deepak@vardhan.test"),
				suresh: testkit.Principal(t, "suresh@vardhan.test"),
			}
			tc.run(t, c)
		})
	}
}

func contractCases() []contractCase {
	return []contractCase{
		{"FILE-001_attach_then_open_returns_bytes_and_meta", caseAttachOpen},
		{"FILE-030_list_newest_first", caseListOrder},
		{"FILE-025_read_follows_parent", caseReadFollowsParent},
		{"FILE-026_uploader_without_read_refused", caseNoOwnerBypass},
		{"FILE-025_hidden_parent_not_found", caseHiddenParent},
		{"FILE-028_unknown_id_not_found", caseUnknownID},
		{"FILE-023_attach_to_missing_parent_refused", caseMissingParent},
		{"FILE-024_attach_without_write_refused", caseAttachNoWrite},
		{"CD-126_remove_without_write_refused", caseRemoveNoWrite},
		{"FILE-040_attach_allowed_in_open_states", caseAttachStates},
		{"FILE-041_attach_to_cancelled_refused", caseAttachCancelled},
		{"FILE-038_FILE-039_remove_only_draft_rejected_or_no_state", caseRemoveStates},
		{"FILE-021_count_limit", caseCountLimit},
		{"FILE-017_same_content_same_parent_refused", caseDuplicate},
		{"FILE-005_empty_refused", caseEmpty},
		{"FILE-007_disallowed_types_refused", caseTypes},
		{"FILE-009_content_mismatch_refused", caseMismatch},
		{"FILE-002_size_limit_boundary", caseSize},
		{"FILE-043_FILE-044_copy_on_amend", caseCopyAll},
		{"FILE-042_delete_all", caseDeleteAll},
	}
}

// ───────── fixtures and helpers ─────────

// po creates a purchase order parent with the given state and display, created by Ravi.
func (c *contract) po(id int64, state, display string) auth.RecordRef {
	return c.s.Parent(doctype.Info{
		DocType: contractPOType, DocLabel: contractPOLabel, ID: id,
		State: doctype.State(state), Display: display, CreatedBy: c.ravi.ID,
	})
}

// writer gives Ravi read and write on purchase orders.
func (c *contract) writer() { c.s.Allow(c.ravi.ID, contractPOType, "read", "write") }

// pdf returns a distinct PDF of n bytes: the last byte is tag.
func pdf(n int, tag byte) []byte {
	b := PDF(n)
	b[len(b)-1] = tag
	return b
}

// attach spools b and attaches it as p inside a write transaction, returning
// exactly what Attach returned.
func (c *contract) attach(t *testing.T, p auth.Principal, ref auth.RecordRef, name string, b []byte) (int64, error) {
	t.Helper()
	sp := c.s.Spool(b)
	var id int64
	var aerr error
	_ = c.s.Write(func(tx *db.Tx) error {
		id, aerr = c.svc.Attach(t.Context(), tx, p, ref, name, sp)
		return aerr
	})
	return id, aerr
}

func (c *contract) mustAttach(t *testing.T, p auth.Principal, ref auth.RecordRef, name string, b []byte) int64 {
	t.Helper()
	id, err := c.attach(t, p, ref, name, b)
	if err != nil {
		t.Fatalf("Attach(%s %d, %q) as %s error = %v, want nil", ref.DocType, ref.ID, name, p.Name, err)
	}
	return id
}

// remove runs Remove as p inside a write transaction, returning what Remove returned.
func (c *contract) remove(t *testing.T, p auth.Principal, id int64) error {
	t.Helper()
	var rerr error
	_ = c.s.Write(func(tx *db.Tx) error {
		rerr = c.svc.Remove(t.Context(), tx, p, id)
		return rerr
	})
	return rerr
}

func (c *contract) list(t *testing.T, p auth.Principal, ref auth.RecordRef) []files.Meta {
	t.Helper()
	ms, err := c.svc.List(t.Context(), p, ref)
	if err != nil {
		t.Fatalf("List(%s %d) as %s error = %v, want nil", ref.DocType, ref.ID, p.Name, err)
	}
	return ms
}

func names(ms []files.Meta) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func (c *contract) open(t *testing.T, p auth.Principal, id int64) ([]byte, files.Meta) {
	t.Helper()
	rc, m, err := c.svc.Open(t.Context(), p, id)
	if err != nil {
		t.Fatalf("Open(%d) as %s error = %v, want nil", id, p.Name, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read Open(%d): %v", id, err)
	}
	return b, m
}

// wantRefused checks err is a *files.Error with kind, HTTP status and message.
func wantRefused(t *testing.T, label string, err error, kind files.Kind, status int, msg string) {
	t.Helper()
	var fe *files.Error
	if !errors.As(err, &fe) {
		t.Errorf("%s: error = %v (%T), want *files.Error kind %d, %d %q", label, err, err, kind, status, msg)
		return
	}
	if fe.Kind != kind {
		t.Errorf("%s: Kind = %d, want %d", label, fe.Kind, kind)
	}
	if fe.Status() != status {
		t.Errorf("%s: Status() = %d, want %d", label, fe.Status(), status)
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", label, err.Error(), msg)
	}
}

// wantOpenRefused opens id as p and wants no reader and the given refusal.
func (c *contract) wantOpenRefused(t *testing.T, label string, p auth.Principal, id int64, kind files.Kind, status int, msg string) {
	t.Helper()
	rc, _, err := c.svc.Open(t.Context(), p, id)
	if rc != nil {
		_ = rc.Close()
		t.Errorf("%s returned a reader, want none", label)
	}
	wantRefused(t, label, err, kind, status, msg)
}

// wantListRefused lists ref as p and wants no rows and the given refusal.
func (c *contract) wantListRefused(t *testing.T, label string, p auth.Principal, ref auth.RecordRef, kind files.Kind, status int, msg string) {
	t.Helper()
	ms, err := c.svc.List(t.Context(), p, ref)
	if len(ms) != 0 {
		t.Errorf("%s = %v, want no rows", label, names(ms))
	}
	wantRefused(t, label, err, kind, status, msg)
}

// wantNames checks the names ref lists for p.
func (c *contract) wantNames(t *testing.T, label string, p auth.Principal, ref auth.RecordRef, want ...string) {
	t.Helper()
	if got := names(c.list(t, p, ref)); !slices.Equal(got, want) {
		t.Errorf("%s: List names = %q, want %q", label, got, want)
	}
}

// ───────── behaviours ─────────

// FILE-001: Open gives back the uploaded bytes and the full Meta.
func caseAttachOpen(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	content := pdf(contractSmallBytes, 1)
	sum := fmt.Sprintf("%x", sha256.Sum256(content))
	at := c.s.Now()
	id := c.mustAttach(t, c.ravi, ref, "bill.pdf", content)
	if id <= 0 {
		t.Errorf("Attach id = %d, want > 0", id)
	}
	b, m := c.open(t, c.ravi, id)
	if !bytes.Equal(b, content) {
		t.Errorf("Open bytes differ from the upload (len %d, want %d)", len(b), len(content))
	}
	want := files.Meta{ID: id, Name: "bill.pdf", MIME: contractPDFMIME, Size: int64(len(content)), SHA256: sum, UploadedBy: c.ravi.ID}
	got := m
	got.UploadedAt = time.Time{}
	if got != want {
		t.Errorf("Open Meta = %+v, want %+v", got, want)
	}
	if !m.UploadedAt.Equal(at) {
		t.Errorf("Open Meta.UploadedAt = %v, want %v", m.UploadedAt, at)
	}
	ms := c.list(t, c.ravi, ref)
	if len(ms) != 1 || ms[0] != m {
		t.Errorf("List = %+v, want exactly Open's Meta %+v", ms, m)
	}
}

// FILE-030: List is newest first.
func caseListOrder(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	c.mustAttach(t, c.ravi, ref, "first.pdf", pdf(contractSmallBytes, 1))
	c.s.Advance(5 * time.Minute)
	c.mustAttach(t, c.ravi, ref, "second.pdf", pdf(contractSmallBytes, 2))
	c.s.Advance(5 * time.Minute)
	c.mustAttach(t, c.ravi, ref, "third.pdf", pdf(contractSmallBytes, 3))
	c.wantNames(t, "three uploads 5 min apart", c.ravi, ref, "third.pdf", "second.pdf", "first.pdf")
	other := c.po(43, "draft", "Draft 43")
	c.wantNames(t, "another parent", c.ravi, other)
}

// FILE-025, FILE-026, CD-126: read on the parent lets anyone read its files;
// without read, Open and List are 403 with AUTH's read text.
func caseReadFollowsParent(t *testing.T, c *contract) {
	c.writer()
	c.s.Allow(c.meena.ID, contractPOType, "read")
	ref := c.po(1, "approved", "PO/26-27/00001")
	content := pdf(contractSmallBytes, 1)
	id := c.mustAttach(t, c.ravi, ref, "quote.pdf", content)

	if b, _ := c.open(t, c.meena, id); !bytes.Equal(b, content) {
		t.Errorf("Open as Meena (read on Purchase Order) bytes differ from the upload")
	}
	c.wantNames(t, "List as Meena (read)", c.meena, ref, "quote.pdf")

	c.wantOpenRefused(t, "Open as Deepak (no read)", c.deepak, id, files.KindForbidden, 403, contractReadPO)
	c.wantListRefused(t, "List as Deepak (no read)", c.deepak, ref, files.KindForbidden, 403, contractReadPO)
}

// FILE-026: the uploader gets nothing extra once read on the parent is gone.
func caseNoOwnerBypass(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	id := c.mustAttach(t, c.ravi, ref, "receipt.pdf", pdf(contractSmallBytes, 1))
	c.s.Deny(c.ravi.ID, contractPOType, "read", "write")
	c.wantOpenRefused(t, "Open as the uploader after losing read", c.ravi, id, files.KindForbidden, 403, contractReadPO)
	c.wantListRefused(t, "List as the uploader after losing read", c.ravi, ref, files.KindForbidden, 403, contractReadPO)
}

// FILE-025, FILE-028, CD-126: a parent the user can't see is 404 "File not
// found." for Open, List and Remove, even with the rights.
func caseHiddenParent(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	id := c.mustAttach(t, c.ravi, ref, "quote.pdf", pdf(contractSmallBytes, 1))
	c.s.Hide(ref)
	c.wantOpenRefused(t, "Open on a hidden parent", c.ravi, id, files.KindNotFound, 404, contractNotFound)
	c.wantListRefused(t, "List on a hidden parent", c.ravi, ref, files.KindNotFound, 404, contractNotFound)
	wantRefused(t, "Remove on a hidden parent", c.remove(t, c.ravi, id), files.KindNotFound, 404, contractNotFound)
}

// FILE-028: an id that doesn't exist is 404 "File not found." for Open and Remove.
func caseUnknownID(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	id := c.mustAttach(t, c.ravi, ref, "quote.pdf", pdf(contractSmallBytes, 1))
	c.wantOpenRefused(t, "Open of an unknown id", c.ravi, id+1000, files.KindNotFound, 404, contractNotFound)
	wantRefused(t, "Remove of an unknown id", c.remove(t, c.ravi, id+1000), files.KindNotFound, 404, contractNotFound)
	c.wantNames(t, "after the refused Remove", c.ravi, ref, "quote.pdf")
}

// FILE-023: a parent that doesn't exist is 404 "Save the document before attaching files."
func caseMissingParent(t *testing.T, c *contract) {
	c.writer()
	ghost := auth.RecordRef{DocType: contractPOType, ID: 999999, CreatedBy: c.ravi.ID}
	_, err := c.attach(t, c.ravi, ghost, "bill.pdf", pdf(contractSmallBytes, 1))
	wantRefused(t, "Attach to a missing parent", err, files.KindParentMissing, 404, contractNoParent)
}

// FILE-024: attaching needs write on the parent; nothing is added.
func caseAttachNoWrite(t *testing.T, c *contract) {
	c.writer()
	c.s.Allow(c.meena.ID, contractPOType, "read")
	ref := c.po(1, "approved", "PO/26-27/00001")
	_, err := c.attach(t, c.meena, ref, "bill.pdf", pdf(contractSmallBytes, 1))
	wantRefused(t, "Attach as Meena (read only)", err, files.KindForbidden, 403, contractAddDenied)
	c.wantNames(t, "after the refused Attach", c.ravi, ref)
}

// CD-126: removing needs write on the parent, worded with AUTH's "change" text.
func caseRemoveNoWrite(t *testing.T, c *contract) {
	c.writer()
	c.s.Allow(c.meena.ID, contractPOType, "read")
	ref := c.po(42, "draft", "Draft 42")
	id := c.mustAttach(t, c.ravi, ref, "quote.pdf", pdf(contractSmallBytes, 1))
	wantRefused(t, "Remove as Meena (read only)", c.remove(t, c.meena, id), files.KindForbidden, 403, contractChangePO)
	c.wantNames(t, "after the refused Remove", c.ravi, ref, "quote.pdf")
}

// FILE-040: adding stays open in every state but cancelled.
func caseAttachStates(t *testing.T, c *contract) {
	c.writer()
	for i, st := range []string{"draft", "pending_approval", "approved", "rejected", "closed", ""} {
		ref := c.po(int64(100+i), st, fmt.Sprintf("Draft %d", 100+i))
		if _, err := c.attach(t, c.ravi, ref, "bill.pdf", pdf(contractSmallBytes, byte(i+1))); err != nil {
			t.Errorf("Attach in state %q error = %v, want nil", st, err)
		}
	}
}

// FILE-041: nothing can be attached to a cancelled document.
func caseAttachCancelled(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(1, "approved", "PO/26-27/00001")
	c.mustAttach(t, c.ravi, ref, "quote.pdf", pdf(contractSmallBytes, 1))
	c.s.SetState(ref, "cancelled")
	_, err := c.attach(t, c.ravi, ref, "bill.pdf", pdf(contractSmallBytes, 2))
	wantRefused(t, "Attach to a cancelled PO", err, files.KindCancelled, 422, contractCancelled)
	c.wantNames(t, "after the refused Attach", c.ravi, ref, "quote.pdf")
}

// FILE-038, FILE-039: Remove works in draft, rejected and stateless parents
// and is refused once submitted.
func caseRemoveStates(t *testing.T, c *contract) {
	c.writer()
	for i, st := range []string{"draft", "rejected", "", "pending_approval", "approved", "closed", "cancelled"} {
		ref := c.po(int64(200+i), "draft", fmt.Sprintf("Draft %d", 200+i))
		id := c.mustAttach(t, c.ravi, ref, "receipt.pdf", pdf(contractSmallBytes, byte(i+1)))
		c.s.SetState(ref, st)
		err := c.remove(t, c.ravi, id)
		switch st {
		case "draft", "rejected", "":
			if err != nil {
				t.Errorf("Remove in state %q error = %v, want nil", st, err)
			}
			c.wantNames(t, fmt.Sprintf("after Remove in state %q", st), c.ravi, ref)
		default:
			wantRefused(t, fmt.Sprintf("Remove in state %q", st), err, files.KindRemoveLocked, 422, contractLocked)
			c.wantNames(t, fmt.Sprintf("after the refused Remove in state %q", st), c.ravi, ref, "receipt.pdf")
		}
	}
}

// FILE-021: at most 20 attachments; removing one makes room again.
func caseCountLimit(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	var first int64
	for i := range files.MaxAttachments {
		id := c.mustAttach(t, c.ravi, ref, fmt.Sprintf("f%02d.pdf", i), pdf(contractSmallBytes, byte(i+1)))
		if i == 0 {
			first = id
		}
	}
	_, err := c.attach(t, c.ravi, ref, "f20.pdf", pdf(contractSmallBytes, 100))
	wantRefused(t, "21st Attach", err, files.KindLimit, 422, contractLimit)
	if n := len(c.list(t, c.ravi, ref)); n != files.MaxAttachments {
		t.Errorf("List after the refused 21st = %d rows, want %d", n, files.MaxAttachments)
	}
	if err := c.remove(t, c.ravi, first); err != nil {
		t.Fatalf("Remove(first) error = %v, want nil", err)
	}
	if _, err := c.attach(t, c.ravi, ref, "f20.pdf", pdf(contractSmallBytes, 100)); err != nil {
		t.Errorf("Attach after removing one error = %v, want nil", err)
	}
}

// FILE-017: the same bytes twice on one parent are refused, under any name.
func caseDuplicate(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	content := pdf(contractSmallBytes, 1)
	c.mustAttach(t, c.ravi, ref, "receipt.pdf", content)
	_, err := c.attach(t, c.ravi, ref, "copy.pdf", content)
	wantRefused(t, "same bytes again", err, files.KindDuplicate, 422, contractDuplicate)
	c.wantNames(t, "after the refused duplicate", c.ravi, ref, "receipt.pdf")
	other := c.po(43, "draft", "Draft 43")
	if _, err := c.attach(t, c.ravi, other, "receipt.pdf", content); err != nil {
		t.Errorf("same bytes on another parent error = %v, want nil (FILE-016)", err)
	}
}

// FILE-005: an empty file is refused.
func caseEmpty(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	_, err := c.attach(t, c.ravi, ref, "blank.pdf", nil)
	wantRefused(t, "blank.pdf (0 bytes)", err, files.KindEmpty, 422, contractEmpty)
	c.wantNames(t, "after the refused empty file", c.ravi, ref)
}

// FILE-007: extensions outside the allowlist are refused with the list.
func caseTypes(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	_, err := c.attach(t, c.ravi, ref, "setup.exe", PE())
	wantRefused(t, "setup.exe", err, files.KindType, 422, contractTypeExe)
	_, err = c.attach(t, c.ravi, ref, "README", pdf(contractSmallBytes, 1))
	wantRefused(t, "README (no extension)", err, files.KindType, 422, contractTypeNone)
	_, err = c.attach(t, c.ravi, ref, "bill.pdf.exe", pdf(contractSmallBytes, 2))
	wantRefused(t, "bill.pdf.exe (FILE-011)", err, files.KindType, 422, contractTypeExe)
	c.wantNames(t, "after the refused types", c.ravi, ref)
}

// FILE-009: content that doesn't match the extension is refused.
func caseMismatch(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	_, err := c.attach(t, c.ravi, ref, "invoice.pdf", HTML())
	wantRefused(t, "invoice.pdf holding HTML", err, files.KindMismatch, 422, contractMismatch)
	c.wantNames(t, "after the refused mismatch", c.ravi, ref)
}

// FILE-002: exactly 10 MB is accepted, one byte more is refused.
func caseSize(t *testing.T, c *contract) {
	c.writer()
	ref := c.po(42, "draft", "Draft 42")
	_, err := c.attach(t, c.ravi, ref, "over.pdf", pdf(int(files.MaxFileBytes)+1, 1))
	wantRefused(t, "10,485,761 bytes", err, files.KindTooLarge, 413, contractTooLarge)
	c.wantNames(t, "after the refused large file", c.ravi, ref)
	if _, err := c.attach(t, c.ravi, ref, "max.pdf", pdf(int(files.MaxFileBytes), 2)); err != nil {
		t.Errorf("10,485,760 bytes error = %v, want nil", err)
	}
}

// FILE-043, FILE-044: CopyAll gives the new draft rows with the same names,
// sizes, types and hashes, uploaded by to.CreatedBy at the time of the amend;
// removing one from the copy leaves the original alone.
func caseCopyAll(t *testing.T, c *contract) {
	c.writer()
	c.s.Allow(c.priya.ID, contractPOType, "read", "write")
	from := c.po(1, "approved", "PO/26-27/00001")
	quote := pdf(contractSmallBytes, 1)
	drawing := pdf(2*contractSmallBytes, 2)
	c.mustAttach(t, c.ravi, from, "quote.pdf", quote)
	c.s.Advance(time.Minute)
	drawingID := c.mustAttach(t, c.ravi, from, "drawing.pdf", drawing)
	c.s.SetState(from, "cancelled")
	orig := c.list(t, c.ravi, from)

	c.s.Advance(time.Hour)
	amendAt := c.s.Now()
	to := c.s.Parent(doctype.Info{
		DocType: contractPOType, DocLabel: contractPOLabel, ID: 43,
		State: doctype.StateDraft, Display: "Draft 43", CreatedBy: c.priya.ID,
	})
	if err := c.s.Write(func(tx *db.Tx) error { return c.svc.CopyAll(t.Context(), tx, from, to) }); err != nil {
		t.Fatalf("CopyAll error = %v, want nil", err)
	}
	copies := c.list(t, c.priya, to)
	if len(copies) != len(orig) {
		t.Fatalf("copies = %d rows %q, want %d", len(copies), names(copies), len(orig))
	}
	for i, m := range copies {
		o := orig[i]
		if m.ID == o.ID {
			t.Errorf("copy %d shares the original's id %d, want a new row", i, m.ID)
		}
		if m.Name != o.Name || m.MIME != o.MIME || m.Size != o.Size || m.SHA256 != o.SHA256 {
			t.Errorf("copy %d = {%q %q %d %s}, want the original's {%q %q %d %s}", i, m.Name, m.MIME, m.Size, m.SHA256, o.Name, o.MIME, o.Size, o.SHA256)
		}
		if m.UploadedBy != c.priya.ID {
			t.Errorf("copy %q UploadedBy = %d, want to.CreatedBy %d (Priya)", m.Name, m.UploadedBy, c.priya.ID)
		}
		if !m.UploadedAt.Equal(amendAt) {
			t.Errorf("copy %q UploadedAt = %v, want the amend time %v", m.Name, m.UploadedAt, amendAt)
		}
	}
	if got := names(copies); !slices.Equal(got, []string{"drawing.pdf", "quote.pdf"}) {
		t.Errorf("copies = %q, want [drawing.pdf quote.pdf] (newest first, original order kept)", got)
	}
	var copyDrawing int64
	for _, m := range copies {
		if m.Name == "drawing.pdf" {
			copyDrawing = m.ID
		}
	}
	if b, _ := c.open(t, c.priya, copyDrawing); !bytes.Equal(b, drawing) {
		t.Errorf("Open(copied drawing.pdf) bytes differ from the original upload")
	}
	if err := c.remove(t, c.priya, copyDrawing); err != nil {
		t.Fatalf("Remove(copied drawing.pdf) from the amended draft error = %v, want nil", err)
	}
	c.wantNames(t, "amended draft after removing drawing.pdf", c.priya, to, "quote.pdf")
	c.wantNames(t, "original after the copy's removal", c.ravi, from, "drawing.pdf", "quote.pdf")
	if b, _ := c.open(t, c.ravi, drawingID); !bytes.Equal(b, drawing) {
		t.Errorf("Open(original drawing.pdf) bytes differ from the upload")
	}
}

// FILE-042: DeleteAll removes every row of the parent and no other's.
func caseDeleteAll(t *testing.T, c *contract) {
	c.writer()
	draft := c.po(42, "draft", "Draft 42")
	other := c.po(43, "draft", "Draft 43")
	shared := pdf(contractSmallBytes, 1)
	a := c.mustAttach(t, c.ravi, draft, "a.pdf", shared)
	b := c.mustAttach(t, c.ravi, draft, "b.pdf", pdf(contractSmallBytes, 2))
	c.mustAttach(t, c.ravi, other, "shared.pdf", shared)
	if err := c.s.Write(func(tx *db.Tx) error { return c.svc.DeleteAll(t.Context(), tx, draft) }); err != nil {
		t.Fatalf("DeleteAll error = %v, want nil", err)
	}
	c.wantNames(t, "deleted draft", c.ravi, draft)
	c.wantNames(t, "other document", c.ravi, other, "shared.pdf")
	c.wantOpenRefused(t, "Open(a.pdf) after DeleteAll", c.ravi, a, files.KindNotFound, 404, contractNotFound)
	c.wantOpenRefused(t, "Open(b.pdf) after DeleteAll", c.ravi, b, files.KindNotFound, 404, contractNotFound)
}
