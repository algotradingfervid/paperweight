package expenseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/testkit"
)

// POST /modules/expense/claims/{id}/receipt (plan MOD-EXP S2; UX §5.5
// "Receipt area"; spec §3.3; CD-133, CD-251). Every case fails before S2:
// the route answers stubs_s2.go's 501.

// s2Fragment checks a successful upload's answer: 200 with only the new
// #receipt-files (the file's thumbnail link, its name and no Remove) and,
// out of band, the line form's hidden receipt_sha256 input with hash, under
// the id the line page gave it (hiddenID). It never holds another input,
// form, file control or the page around it, so nothing being typed is
// redrawn (UX §5.5, CLAUDE.md).
func s2Fragment(t *testing.T, what string, rec *httptest.ResponseRecorder, m files.Meta, hiddenID string) *goquery.Document {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200 (body %q)", what, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("%s: Content-Type %q, want text/html", what, ct)
	}
	if rec.Header().Get("HX-Refresh") != "" || rec.Header().Get("HX-Reswap") != "" {
		t.Errorf("%s: HX-Refresh %q, HX-Reswap %q; want neither (a reload loses the typed fields)", what, rec.Header().Get("HX-Refresh"), rec.Header().Get("HX-Reswap"))
	}
	body := rec.Body.String()
	for _, whole := range []string{"<html", "<body", "<main", `id="line-form"`, `id="receipt"`} {
		if strings.Contains(body, whole) {
			t.Errorf("%s: the answer holds %q, want only #receipt-files and the hidden hash", what, whole)
		}
	}
	d := s2Parse(t, rec)
	files := d.Find("#receipt-files")
	if files.Length() != 1 {
		t.Fatalf("%s: %d #receipt-files in the answer, want 1 (body %q)", what, files.Length(), body)
	}
	href := "/files/" + strconv.FormatInt(m.ID, 10)
	if files.Find(`a[href="`+href+`"]`).Length() == 0 {
		t.Errorf("%s: #receipt-files has no link to %s (body %q)", what, href, body)
	}
	if strings.HasPrefix(m.MIME, "image/") && files.Find(`a[href="`+href+`"] img`).Length() == 0 {
		t.Errorf("%s: #receipt-files shows no thumbnail of %s", what, m.Name)
	}
	if !strings.Contains(s2Text(files), m.Name) {
		t.Errorf("%s: #receipt-files text %q doesn't name %s", what, s2Text(files), m.Name)
	}
	if strings.Contains(s2Text(files), "Remove") || files.Find("dialog, [data-open-dialog]").Length() > 0 {
		t.Errorf("%s: #receipt-files offers Remove; FILE's remove reloads the page (UX §5.5)", what)
	}
	if n := d.Find("form, textarea, select, button").Length(); n != 0 {
		t.Errorf("%s: the answer holds %d forms, textareas, selects or buttons, want none", what, n)
	}
	inputs := d.Find("input")
	if inputs.Length() != 1 {
		t.Errorf("%s: the answer holds %d inputs, want only the hidden receipt_sha256 (body %q)", what, inputs.Length(), body)
	}
	h := d.Find(`input[name="receipt_sha256"]`)
	if h.Length() != 1 {
		t.Fatalf("%s: %d receipt_sha256 inputs in the answer, want 1 (body %q)", what, h.Length(), body)
	}
	if h.AttrOr("type", "") != "hidden" || h.AttrOr("value", "") != m.SHA256 {
		t.Errorf("%s: receipt_sha256 is type %q value %q, want hidden %s", what, h.AttrOr("type", ""), h.AttrOr("value", ""), m.SHA256)
	}
	if _, oob := h.Attr("hx-swap-oob"); !oob {
		t.Errorf("%s: receipt_sha256 has no hx-swap-oob; it must replace the line form's input out of band", what)
	}
	if hiddenID != "" && h.AttrOr("id", "") != hiddenID {
		t.Errorf("%s: receipt_sha256 id %q, want the line form's %q", what, h.AttrOr("id", ""), hiddenID)
	}
	if h.ParentsFiltered("#receipt-files").Length() != 0 {
		t.Errorf("%s: receipt_sha256 sits inside #receipt-files; it belongs to the line form", what)
	}
	return d
}

// A photo uploaded from the line page is attached to the claim (Spool then
// Attach, spec §3.3), the claim's version doesn't change (FILE-040), and the
// answer is the fragment only, with the hash under the line form's input id.
func TestExpReceipt_Upload(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	v := e.version(t, c.ID)
	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")
	hiddenID := s2HiddenReceipt(t, s2LineForm(t, page)).AttrOr("id", "")

	body := filestest.JPEG(4096)
	rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(v), s2File("bill.jpg", body))
	got := e.FilesOf(t, expense.ClaimDocType, c.ID)
	if len(got) != 1 || got[0].Name != "bill.jpg" || got[0].Size != int64(len(body)) {
		t.Fatalf("after the upload the claim has %+v, want bill.jpg of %d bytes", got, len(body))
	}
	if got[0].UploadedBy != deepak.ID {
		t.Errorf("bill.jpg uploaded by %d, want Deepak %d", got[0].UploadedBy, deepak.ID)
	}
	s2Fragment(t, "upload bill.jpg", rec, got[0], hiddenID)
	if nv := e.version(t, c.ID); nv != v {
		t.Errorf("claim version %d after the upload, want %d unchanged (FILE-040), so Save line doesn't conflict", nv, v)
	}

	// A PDF answers with its file chip and the same hidden hash.
	pdf := filestest.PDF(5000)
	rec = e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(v), s2File("hotel.pdf", pdf))
	var hotel files.Meta
	for _, m := range e.FilesOf(t, expense.ClaimDocType, c.ID) {
		if m.Name == "hotel.pdf" {
			hotel = m
		}
	}
	if hotel.ID == 0 {
		t.Fatalf("hotel.pdf wasn't attached (status %d, body %q)", rec.Code, rec.Body.String())
	}
	s2Fragment(t, "upload hotel.pdf", rec, hotel, hiddenID)
}

// The route's refusals (CD-133, CD-251, CD-94): FILE's texts as an htmx
// notice, AUTH's 403 for write on the claim and 404 for a claim the user
// can't read, DOC's 409 for a stale version and 400 for a missing one; each
// writes nothing. The right is checked before the version (lessons §11).
// Deepak and Priya are non-admins on the real auth.Service; Priya reads
// Deepak's claim (team) but writes only her own.
func TestExpReceipt_Refusals(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	if _, err := e.EditClaim(t, deepak, c.ID, expensetest.Values{"purpose": "Site visit, Pune"}, nil); err != nil {
		t.Fatalf("edit claim: %v", err)
	}
	id := strconv.FormatInt(c.ID, 10)
	v := strconv.Itoa(e.version(t, c.ID))
	stale := strconv.Itoa(e.version(t, c.ID) - 1)
	jpg := s2File("bill.jpg", filestest.JPEG(4096))

	for _, tc := range []struct {
		name, email, id, version string
		parts                    []s2Part
		status                   int
		msg                      string
	}{
		{"two file parts", s2Deepak, id, v, []s2Part{jpg, s2File("b.jpg", filestest.JPEG(5000))}, http.StatusUnprocessableEntity, s2MsgTwoParts},
		{"over 10 MB", s2Deepak, id, v, []s2Part{s2File("big.jpg", filestest.JPEG(int(files.MaxFileBytes)+1))}, http.StatusRequestEntityTooLarge, s2MsgTooLarge},
		{"a GIF", s2Deepak, id, v, []s2Part{s2File("receipt.gif", append([]byte("GIF89a"), make([]byte, 2048)...))}, http.StatusUnprocessableEntity, s2MsgGIF},
		{"no write on the claim", s2Priya, id, v, []s2Part{jpg}, http.StatusForbidden, s2MsgOwnWrite},
		{"no write, stale version", s2Priya, id, stale, []s2Part{jpg}, http.StatusForbidden, s2MsgOwnWrite},
		{"a claim she can't read", s2Kavya, id, v, []s2Part{jpg}, http.StatusNotFound, auth.MsgNotFound},
		{"can't read, stale version", s2Kavya, id, stale, []s2Part{jpg}, http.StatusNotFound, auth.MsgNotFound},
		{"a missing claim", s2Deepak, strconv.FormatInt(c.ID+100, 10), v, []s2Part{jpg}, http.StatusNotFound, auth.MsgNotFound},
		{"a bad id", s2Deepak, "x", v, []s2Part{jpg}, http.StatusNotFound, auth.MsgNotFound},
		{"a stale version", s2Deepak, id, stale, []s2Part{jpg}, http.StatusConflict, s2MsgStale(c.ID)},
		{"no version", s2Deepak, id, "", []s2Part{jpg}, http.StatusBadRequest, ""},
		{"a version that isn't a number", s2Deepak, id, "abc", []s2Part{jpg}, http.StatusBadRequest, ""},
	} {
		e.refused(t, tc.name, tc.status, tc.msg, func() *httptest.ResponseRecorder {
			return e.upload(t, tc.email, tc.id, tc.version, tc.parts...)
		})
	}
	if got := e.FilesOf(t, expense.ClaimDocType, c.ID); len(got) != 0 {
		t.Errorf("after the refusals the claim has %d attachments, want 0", len(got))
	}
}

// A claim takes 100 attachments, not FILE's default 20 (spec §3.3): through
// the route the 21st is accepted and the 101st refused with the 100 text,
// nothing written.
func TestExpReceipt_Limit100(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	id, v := strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID))
	for i := range 20 {
		e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "r"+strconv.Itoa(i)+".pdf", filestest.PDF(1024+i))
	}
	if rec := e.upload(t, s2Deepak, id, v, s2File("r20.jpg", filestest.JPEG(4096))); rec.Code != http.StatusOK {
		t.Fatalf("the 21st upload: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	for i := 21; i < 100; i++ {
		e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "r"+strconv.Itoa(i)+".pdf", filestest.PDF(1024+i))
	}
	if n := len(e.FilesOf(t, expense.ClaimDocType, c.ID)); n != 100 {
		t.Fatalf("the claim has %d attachments, want 100", n)
	}
	e.refused(t, "the 101st upload", http.StatusUnprocessableEntity, s2MsgLimit100, func() *httptest.ResponseRecorder {
		return e.upload(t, s2Deepak, id, v, s2File("r100.jpg", filestest.JPEG(5000)))
	})
}

// The version check, Attach and the answer's reads use the one write
// transaction (CD-94, CD-209, CD-213): as a non-admin on a pool of one
// connection, the upload succeeds and a stale one is refused.
func TestExpReceipt_NoSecondConn(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	id, v := strconv.FormatInt(c.ID, 10), e.version(t, c.ID)

	// Each request is built outside the pool cut, then served inside it.
	build := func(version int, name string) *http.Request {
		t.Helper()
		return e.uploadRequest(t, id, strconv.Itoa(version), s2File(name, s2Bytes(name)))
	}
	run := func(req *http.Request) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		if err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
			e.router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(ctx, deepak)))
			return nil
		}); err != nil {
			t.Fatalf("NoSecondConn: %v", err)
		}
		return rec
	}

	rec := run(build(v, "taxi.jpg"))
	got := e.FilesOf(t, expense.ClaimDocType, c.ID)
	if len(got) != 1 {
		t.Fatalf("after the upload the claim has %d attachments, want 1 (status %d, body %q)", len(got), rec.Code, rec.Body.String())
	}
	s2Fragment(t, "upload on one connection", rec, got[0], "")

	req := build(v-1, "bus.jpg")
	e.refused(t, "stale upload on one connection", http.StatusConflict, s2MsgStale(c.ID), func() *httptest.ResponseRecorder {
		return run(req)
	})
}

// The same bytes chosen again on one claim (FILE refuses identical bytes on
// one parent): the route answers as a success with the attached file and
// UX §5.5's note, naming the lowest line holding it, and writes nothing
// (CD-270 Q6, EUX-004).
func TestExpReceipt_SameBytes(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil, nil, nil, nil)
	metas := e.withReceipts(t, c.ID, map[int]string{2: "bill.jpg", 3: "bill.jpg"})
	spare := e.MustAttach(t, deepak, expense.ClaimDocType, c.ID, "spare.jpg", s2Bytes("spare.jpg"))
	id, v := strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID))
	hiddenID := s2HiddenReceipt(t, s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new"))).AttrOr("id", "")

	for _, tc := range []struct {
		what, name string
		m          files.Meta
		note       string
	}{
		{"bill.jpg again (lines 2 and 3 hold it)", "bill-again.jpg", metas["bill.jpg"], s2MsgReuseLine},
		{"spare.jpg again (no line holds it)", "spare-again.jpg", spare, s2MsgReuseNone},
	} {
		var rec *httptest.ResponseRecorder
		e.WantRefused(t, "-", func() error {
			rec = e.upload(t, s2Deepak, id, v, s2File(tc.name, s2Bytes(tc.m.Name)))
			return s2Refusal("-")
		})
		d := s2Fragment(t, tc.what, rec, tc.m, hiddenID)
		if got := s2Text(d.Find("#receipt-files")); !strings.Contains(got, tc.note) {
			t.Errorf("%s: #receipt-files reads %q, want the note %q", tc.what, got, tc.note)
		}
		if got := s2Text(d.Find("#receipt-files")); strings.Contains(got, "already attached") {
			t.Errorf("%s: #receipt-files carries FILE's duplicate refusal %q", tc.what, got)
		}
	}
}
