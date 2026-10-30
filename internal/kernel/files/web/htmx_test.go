package web_test

// Design 2026-10-31-document-attachments §6 change 3 (CD-241): with
// HX-Request: true a success answers with HX-Refresh and a refusal keeps its
// status and text but sends an empty body, HX-Reswap: none and an HX-Trigger
// notice. Without the header nothing changes.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgParentMissing = "Save the document before attaching files."
	msgTypeGIF       = "File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	msgTypeDevanagri = "File type .रसीद is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	msgMismatchPDF   = "File content does not match its .pdf extension. Upload the original file."
	msgCSVEncoding   = "CSV files must be UTF-8. Save the file as CSV UTF-8 and try again."
	msgDuplicate     = "This file is already attached to Draft 42."
	msgLimit         = "A document can have at most 20 attachments. Remove one first."
	msgCancelled     = "This document is cancelled. Attach files to its amended copy instead."
	msgInUseLine2    = "This file is the receipt on line 2. Clear it there first."
	msgBusy          = "The system is busy. Please try again in a moment."
)

// asHTMX marks req as an htmx request when on.
func asHTMX(req *http.Request, on bool) *http.Request {
	if on {
		req.Header.Set("HX-Request", "true")
	}
	return req
}

func removeReqAs(id string, p *auth.Principal) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/files/"+id+"/remove", nil)
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *p))
	}
	return req
}

func mustAttachFile(t *testing.T, f *fixture, name string, b []byte) int64 {
	t.Helper()
	id, err := f.env.Attach(f.deepak, f.exp, name, b)
	if err != nil {
		t.Fatalf("Attach(%s): %v", name, err)
	}
	return id
}

// wantNotice checks an htmx refusal: status, empty body, HX-Reswap none, an
// ASCII HX-Trigger that decodes to exactly the error notice, and no HX-Refresh.
func wantNotice(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status = %d, want %d", what, rec.Code, status)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("%s: body = %q, want empty", what, rec.Body.String())
	}
	if got := rec.Header().Values("HX-Reswap"); len(got) != 1 || got[0] != "none" {
		t.Errorf("%s: HX-Reswap = %q, want exactly [\"none\"]", what, got)
	}
	if got := rec.Header().Get("HX-Refresh"); got != "" {
		t.Errorf("%s: HX-Refresh = %q, want none on a refusal", what, got)
	}
	trigger := rec.Header().Values("HX-Trigger")
	if len(trigger) != 1 {
		t.Errorf("%s: HX-Trigger = %q, want exactly one", what, trigger)
		return
	}
	for i := 0; i < len(trigger[0]); i++ {
		if trigger[0][i] > 0x7F {
			t.Errorf("%s: HX-Trigger = %q has non-ASCII byte 0x%X at %d", what, trigger[0], trigger[0][i], i)
			break
		}
	}
	var got any
	if err := json.Unmarshal([]byte(trigger[0]), &got); err != nil {
		t.Errorf("%s: HX-Trigger = %q is not JSON: %v", what, trigger[0], err)
		return
	}
	want := map[string]any{"notice": map[string]any{"level": "error", "text": msg}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: HX-Trigger decodes to %v, want %v", what, got, want)
	}
}

// wantPlain checks a non-htmx refusal is unchanged: plain text with its status
// and none of the htmx headers.
func wantPlain(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	wantText(t, what, rec, status, msg)
	for _, h := range []string{"HX-Reswap", "HX-Trigger", "HX-Refresh"} {
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("%s: %s = %q, want none without HX-Request", what, h, got)
		}
	}
}

// ATT-008
func TestUpload_HTMXRefresh(t *testing.T) {
	t.Run("htmx", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := serve(f, asHTMX(uploadReq(t, uploadPath, &f.deepak, part{"file", "receipt.jpg", filestest.JPEG(4096)}), true))
		if rec.Code != http.StatusCreated {
			t.Errorf("htmx upload: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Values("HX-Refresh"); len(got) != 1 || got[0] != "true" {
			t.Errorf("htmx upload: HX-Refresh = %q, want exactly [\"true\"]", got)
		}
		if rows := f.env.Rows(f.exp); len(rows) != 1 || rows[0].Name != "receipt.jpg" {
			t.Errorf("rows = %+v, want receipt.jpg", rows)
		}
	})

	t.Run("plain", func(t *testing.T) {
		f := newFixture(t, nil)
		content := filestest.JPEG(4096)
		rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", "receipt.jpg", content}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("HX-Refresh"); got != "" {
			t.Errorf("upload without HX-Request: HX-Refresh = %q, want none", got)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
		}
		rows := f.env.Rows(f.exp)
		if len(rows) != 1 {
			t.Fatalf("rows = %+v, want 1", rows)
		}
		want := map[string]any{
			"id":           float64(rows[0].ID),
			"file_name":    "receipt.jpg",
			"size":         float64(len(content)),
			"content_type": "image/jpeg",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body = %v, want exactly %v", got, want)
		}
	})
}

// ATT-009
func TestRemove_HTMXRefresh(t *testing.T) {
	t.Run("htmx", func(t *testing.T) {
		f := newFixture(t, nil)
		id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
		rec := serve(f, asHTMX(removeReqAs(strconv.FormatInt(id, 10), &f.deepak), true))
		if rec.Code != http.StatusNoContent {
			t.Errorf("htmx remove: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Values("HX-Refresh"); len(got) != 1 || got[0] != "true" {
			t.Errorf("htmx remove: HX-Refresh = %q, want exactly [\"true\"]", got)
		}
		wantNoRows(t, f)
	})

	t.Run("plain", func(t *testing.T) {
		f := newFixture(t, nil)
		id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
		rec := serve(f, removeReqAs(strconv.FormatInt(id, 10), &f.deepak))
		if rec.Code != http.StatusNoContent {
			t.Errorf("remove: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("HX-Refresh"); got != "" {
			t.Errorf("remove without HX-Request: HX-Refresh = %q, want none", got)
		}
		wantNoRows(t, f)
	})
}

// refusalCase builds its own fixture and serves one refused request, marked as
// htmx when hx.
type refusalCase struct {
	name   string
	status int
	msg    string
	run    func(t *testing.T, hx bool) *httptest.ResponseRecorder
}

// uploadAs serves an upload of parts to target by p on a fresh fixture after prep.
func uploadAs(target string, who func(f *fixture) *auth.Principal, prep func(t *testing.T, f *fixture), parts ...part) func(t *testing.T, hx bool) *httptest.ResponseRecorder {
	return func(t *testing.T, hx bool) *httptest.ResponseRecorder {
		f := newFixture(t, nil)
		if prep != nil {
			prep(t, f)
		}
		return serve(f, asHTMX(uploadReq(t, target, who(f), parts...), hx))
	}
}

// removeAs attaches receipt.jpg to Draft 42 on a fresh fixture, runs prep and
// serves a remove of target ("" means the attached file) by p.
func removeAs(target string, who func(f *fixture) *auth.Principal, prep func(t *testing.T, f *fixture)) func(t *testing.T, hx bool) *httptest.ResponseRecorder {
	return func(t *testing.T, hx bool) *httptest.ResponseRecorder {
		f := newFixture(t, nil)
		id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
		if prep != nil {
			prep(t, f)
		}
		if target == "" {
			target = strconv.FormatInt(id, 10)
		}
		return serve(f, asHTMX(removeReqAs(target, who(f)), hx))
	}
}

func deepak(f *fixture) *auth.Principal { return &f.deepak }

// raviReadOnly is Ravi with only read on expense.
func raviReadOnly(f *fixture) *auth.Principal {
	ravi := f.env.User("ravi@vardhan.test")
	f.env.Auth.Allow(ravi.ID, "expense", "read")
	return &ravi
}

func pdfPart(name string) part { return part{"file", name, filestest.PDF(4096)} }

// ATT-010: every server refusal of §5.1's table reachable here (the dropped
// connection has no answer). Each runs once with HX-Request (a notice) and once
// without (plain text, unchanged).
func TestFiles_HTMXRefusalNotices(t *testing.T) {
	// "code,name\n" then an invalid UTF-8 pair, as in TestSniff_CSVTextRules.
	notUTF8CSV := append([]byte("code,name\n"), 0xC3, 0x28, '\n')

	cases := []refusalCase{
		{"upload without write", http.StatusForbidden, msgNoAddRight,
			uploadAs(uploadPath, raviReadOnly, nil, pdfPart("bill.pdf"))},
		{"remove without write", http.StatusForbidden, msgNoChange,
			removeAs("", raviReadOnly, nil)},
		{"upload to a missing document", http.StatusNotFound, msgParentMissing,
			uploadAs("/attachments/expense/999999", deepak, nil, pdfPart("bill.pdf"))},
		{"upload to a hidden document", http.StatusNotFound, msgParentMissing,
			uploadAs(uploadPath, deepak, func(_ *testing.T, f *fixture) { f.env.Auth.Hide("expense", 42) }, pdfPart("bill.pdf"))},
		{"upload with a bad id", http.StatusNotFound, msgFileNotFound,
			uploadAs("/attachments/expense/abc", deepak, nil, pdfPart("bill.pdf"))},
		{"remove of a gone file", http.StatusNotFound, msgFileNotFound,
			removeAs("999999", deepak, nil)},
		{"remove with a bad id", http.StatusNotFound, msgFileNotFound,
			removeAs("abc", deepak, nil)},
		{"remove on a hidden document", http.StatusNotFound, msgFileNotFound,
			removeAs("", deepak, func(_ *testing.T, f *fixture) { f.env.Auth.Hide("expense", 42) })},
		{"declared size over the limit", http.StatusRequestEntityTooLarge, msgTooLarge,
			func(t *testing.T, hx bool) *httptest.ResponseRecorder {
				f := newFixture(t, nil)
				req := uploadReq(t, uploadPath, &f.deepak, pdfPart("bill.pdf"))
				req.ContentLength = 52428800
				return serve(f, asHTMX(req, hx))
			}},
		{"10,485,761-byte file", http.StatusRequestEntityTooLarge, msgTooLarge,
			uploadAs(uploadPath, deepak, nil, part{"file", "big.pdf", filestest.PDF(int(files.MaxFileBytes) + 1)})},
		{"empty file", http.StatusUnprocessableEntity, msgEmpty,
			uploadAs(uploadPath, deepak, nil, part{"file", "bill.pdf", nil})},
		{"type .gif", http.StatusUnprocessableEntity, msgTypeGIF,
			uploadAs(uploadPath, deepak, nil, part{"file", "logo.gif", []byte("GIF89a\x01\x00\x01\x00")})},
		{"type .रसीद (non-ASCII text)", http.StatusUnprocessableEntity, msgTypeDevanagri,
			uploadAs(uploadPath, deepak, nil, pdfPart("बिल.रसीद"))},
		{"content does not match", http.StatusUnprocessableEntity, msgMismatchPDF,
			uploadAs(uploadPath, deepak, nil, part{"file", "invoice.pdf", filestest.HTML()})},
		{"CSV not UTF-8", http.StatusUnprocessableEntity, msgCSVEncoding,
			uploadAs(uploadPath, deepak, nil, part{"file", "items.csv", notUTF8CSV})},
		{"same bytes already attached", http.StatusUnprocessableEntity, msgDuplicate,
			uploadAs(uploadPath, deepak, func(t *testing.T, f *fixture) {
				mustAttachFile(t, f, "receipt.pdf", filestest.PDF(4096))
			}, pdfPart("copy.pdf"))},
		{"count limit", http.StatusUnprocessableEntity, msgLimit,
			uploadAs(uploadPath, deepak, func(t *testing.T, f *fixture) {
				for i := range files.MaxAttachments {
					mustAttachFile(t, f, fmt.Sprintf("f%02d.pdf", i), filestest.PDF(4096+i+1))
				}
			}, pdfPart("f20.pdf"))},
		{"upload to a cancelled document", http.StatusUnprocessableEntity, msgCancelled,
			uploadAs(uploadPath, deepak, func(_ *testing.T, f *fixture) { f.env.SetState(f.exp, "cancelled") }, pdfPart("bill.pdf"))},
		{"remove after submit", http.StatusUnprocessableEntity, msgRemoveLocked,
			removeAs("", deepak, func(_ *testing.T, f *fixture) { f.env.SetState(f.exp, "pending_approval") })},
		{"remove of a line's receipt", http.StatusUnprocessableEntity, msgInUseLine2,
			func(t *testing.T, hx bool) *httptest.ResponseRecorder {
				f := newFixture(t, nil)
				id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
				store, err := files.New(files.Config{
					DataDir: f.env.DataDir, DB: f.env.DB, Auth: f.env.Auth, Info: f.env.Docs,
					Clock: f.env.Clock, Audit: f.env.Audit,
					RemoveGuards: map[string]files.RemoveGuard{
						"expense": func(context.Context, *db.Tx, int64, int64) (int, error) { return 2, nil },
					},
				})
				if err != nil {
					t.Fatalf("files.New with a RemoveGuard: %v", err)
				}
				r := web.Routes(web.Config{Store: store, DB: f.env.DB, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, asHTMX(removeReqAs(strconv.FormatInt(id, 10), &f.deepak), hx))
				return rec
			}},
		{"no file part", http.StatusUnprocessableEntity, msgNoFilePart,
			uploadAs(uploadPath, deepak, nil, part{"note", "bill.pdf", filestest.PDF(4096)})},
		{"two file parts", http.StatusUnprocessableEntity, msgTwoFileParts,
			uploadAs(uploadPath, deepak, nil, pdfPart("bill.pdf"), part{"file", "r.png", filestest.PNG(4096)})},
		{"busy", http.StatusServiceUnavailable, msgBusy,
			func(t *testing.T, hx bool) *httptest.ResponseRecorder {
				f := newFixture(t, nil)
				id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
				short, err := db.Open(f.env.DB.Path(), db.WithBusyTimeout(100*time.Millisecond))
				if err != nil {
					t.Fatalf("db.Open with a short busy timeout: %v", err)
				}
				defer func() { _ = short.Close() }()
				holder, err := f.env.DB.SQL().Conn(t.Context())
				if err != nil {
					t.Fatalf("holder conn: %v", err)
				}
				defer func() { _ = holder.Close() }()
				if _, err := holder.ExecContext(t.Context(), "BEGIN IMMEDIATE"); err != nil {
					t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
				}
				defer func() { _, _ = holder.ExecContext(context.Background(), "ROLLBACK") }()
				r := web.Routes(web.Config{Store: f.env.Store, DB: short, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, asHTMX(removeReqAs(strconv.FormatInt(id, 10), &f.deepak), hx))
				return rec
			}},
		{"storage failure", http.StatusInternalServerError, msgStorage,
			uploadAs(uploadPath, deepak, func(t *testing.T, f *fixture) {
				err := f.env.DB.Write(t.Context(), func(tx *db.Tx) error {
					_, err := tx.SQL().ExecContext(t.Context(),
						`CREATE TRIGGER fail_attach BEFORE INSERT ON attachment BEGIN SELECT RAISE(ABORT, 'forced'); END;`)
					return err
				})
				if err != nil {
					t.Fatalf("create trigger: %v", err)
				}
			}, pdfPart("bill.pdf"))},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/htmx", func(t *testing.T) {
			wantNotice(t, tc.name+" (htmx)", tc.run(t, true), tc.status, tc.msg)
		})
		t.Run(tc.name+"/plain", func(t *testing.T) {
			wantPlain(t, tc.name, tc.run(t, false), tc.status, tc.msg)
		})
	}
}

// The signed-out 401 keeps AUTH's answer for htmx: HX-Redirect to sign in and
// the plain text, no notice (CD-17, CD-63; unchanged by CD-241).
func TestFiles_HTMXSignedOutKeepsRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, redirect string
		req            func(t *testing.T, f *fixture) *http.Request
	}{
		{"upload", "/signin?next=%2Fattachments%2Fexpense%2F42", func(t *testing.T, _ *fixture) *http.Request {
			return uploadReq(t, uploadPath, nil, pdfPart("bill.pdf"))
		}},
		{"remove", "/signin?next=%2Ffiles%2F1%2Fremove", func(_ *testing.T, _ *fixture) *http.Request {
			return removeReqAs("1", nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			rec := serve(f, asHTMX(tc.req(t, f), true))
			wantText(t, "signed-out htmx "+tc.name, rec, http.StatusUnauthorized, msgExpired)
			if got := rec.Header().Get("HX-Redirect"); got != tc.redirect {
				t.Errorf("HX-Redirect = %q, want %q", got, tc.redirect)
			}
			for _, h := range []string{"HX-Reswap", "HX-Trigger", "HX-Refresh"} {
				if got := rec.Header().Get(h); got != "" {
					t.Errorf("%s = %q, want none on the signed-out answer", h, got)
				}
			}
		})
	}
}

// ATT-012 (server half): a crafted remove on an approved document is refused
// with FILE's text, as a notice for htmx and as plain text otherwise, and the
// file stays.
func TestRemove_AfterApprovalRefused(t *testing.T) {
	for _, hx := range []bool{true, false} {
		t.Run(map[bool]string{true: "htmx", false: "plain"}[hx], func(t *testing.T) {
			f := newFixture(t, nil)
			id := mustAttachFile(t, f, "receipt.jpg", filestest.JPEG(4096))
			f.env.SetState(f.exp, "approved")
			rec := serve(f, asHTMX(removeReqAs(strconv.FormatInt(id, 10), &f.deepak), hx))
			if hx {
				wantNotice(t, "htmx remove on an approved document", rec, http.StatusUnprocessableEntity, msgRemoveLocked)
			} else {
				wantPlain(t, "remove on an approved document", rec, http.StatusUnprocessableEntity, msgRemoveLocked)
			}
			if rows := f.env.Rows(f.exp); len(rows) != 1 {
				t.Errorf("rows after the refused remove = %+v, want receipt.jpg kept", rows)
			}
		})
	}
}
