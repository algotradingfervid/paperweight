package mstweb_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: the generic screens' handling of what a master's service
// returns. A stub service stands in for the master; Item's DocType supplies
// the fields, because it has Link fields and line fields.

const (
	base   = "/m/stub"
	record = base + "/7"
)

// stubRecord is a stored record: its Values, as a master would give them.
type stubRecord map[string]string

// stubService returns getErr from Get, and err from every write. It writes
// nothing, so a nil err isn't used here.
type stubService struct {
	rec    stubRecord
	getErr error
	err    error
}

func (s stubService) Create(context.Context, auth.Principal, stubRecord) (mstkit.Saved, error) {
	return mstkit.Saved{}, s.err
}

func (s stubService) Update(context.Context, auth.Principal, int64, int, stubRecord) (mstkit.Saved, error) {
	return mstkit.Saved{}, s.err
}

func (s stubService) SetDisabled(context.Context, auth.Principal, int64, int, bool) error {
	return s.err
}

func (s stubService) Delete(context.Context, auth.Principal, int64, int) error { return s.err }

func (s stubService) Get(context.Context, *db.Tx, int64) (stubRecord, error) {
	return s.rec, s.getErr
}

// refusal is an HTTPError with its own text, status and field errors.
type refusal struct {
	text   string
	status int
	fields []doctype.FieldError
}

func (e *refusal) Error() string                     { return e.text }
func (e *refusal) Status() int                       { return e.status }
func (e *refusal) FieldErrors() []doctype.FieldError { return e.fields }

func harness(t *testing.T, svc stubService) *webtest.Harness {
	t.Helper()
	return webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		return mstweb.Screen[stubRecord, stubRecord]{
			DocType: item.DocType,
			Base:    base,
			Copy:    mstweb.Copy{Plural: "Stubs", New: "New stub", Search: "Search stubs", Empty: "No stubs yet."},
			Svc:     svc,
			Env:     k,
			ToInput: func(v map[string]string) stubRecord { return v },
			// A copy, as a master builds a fresh map for each call.
			Values: func(r stubRecord) map[string]string { return maps.Clone(r) },
			Head:   func(stubRecord) mstweb.Head { return mstweb.Head{Code: "STUB", Name: "Stub", Version: 3} },
			Ref:    func(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: item.DocType, ID: id} },
		}.Routes()
	})
}

// A field error with no field has no input to sit next to: it is a banner
// on the form, which keeps what was typed.
func TestScreens_FieldlessErrorIsABanner(t *testing.T) {
	h := harness(t, stubService{err: &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Message: "The record as a whole is wrong."},
		{Field: "name", Message: "Name is wrong."},
	}}})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"name": {"Typed"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, resp, "The record as a whole is wrong.")
	webtest.WantFieldError(t, resp, "name", "Name is wrong.")
	if v, _ := resp.Value("name"); v != "Typed" {
		t.Errorf("name value = %q; want the typed Typed", v)
	}
}

// An HTTPError's own text is what the page shows, stale (with Reload) or not.
func TestScreens_HTTPErrorTextIsShown(t *testing.T) {
	for _, tc := range []struct {
		status int
		reload bool
	}{{http.StatusUnprocessableEntity, false}, {http.StatusConflict, true}} {
		h := harness(t, stubService{rec: stubRecord{}, err: &refusal{text: "Producer's own words.", status: tc.status}})
		resp := h.Post(t, h.Env.Kavya, record, url.Values{"version": {"3"}, "name": {"Typed"}})
		webtest.WantStatus(t, resp, tc.status)
		webtest.WantBanner(t, resp, "Producer's own words.")
		if _, got := resp.RowLink(record); got != tc.reload {
			t.Errorf("status %d: Reload link found = %v; want %v", tc.status, got, tc.reload)
		}
		if v, _ := resp.Value("name"); v != "Typed" {
			t.Errorf("status %d: name value = %q; want the typed Typed", tc.status, v)
		}
	}
}

// A refusal the form can't show (403, or an HTTPError of another status)
// is a page with the producer's text.
func TestScreens_OtherHTTPErrorIsAProblemPage(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}, err: &refusal{text: "Producer's own words.", status: http.StatusForbidden}})
	resp := h.Post(t, h.Env.Kavya, record, url.Values{"version": {"3"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, "Producer's own words.")
	if _, ok := resp.RowLink(base); !ok {
		t.Errorf("problem page has no Back link to %s", base)
	}
}

// A read that fails with a plain error is web.ErrorFrom's 500, without the
// error's text.
func TestScreens_InternalErrorIsNotShown(t *testing.T) {
	h := harness(t, stubService{getErr: errors.New("secret detail")})
	resp := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, resp, http.StatusInternalServerError)
	webtest.WantNoText(t, resp, "secret detail")
}

// A line row's field error links to the input's id from its row number and
// field, as docscreen names line inputs.
func TestScreens_LineErrorLinksToStableID(t *testing.T) {
	h := harness(t, stubService{err: &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Field: "uom_id", Line: 2, Message: "Row 2: UoM is required."},
	}}})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	text, ok := resp.RowLink("#f-lines-2-uom_id")
	if !ok || text != "Row 2: UoM is required." {
		t.Errorf("summary link to #f-lines-2-uom_id = %q (found %v)", text, ok)
	}
}

// Writers get the form, so the display text isn't computed: it would look
// up this missing link and fail the page. Readers need it.
func TestScreens_DisplayPairsOnlyForReaders(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"stock_uom_id": "999999"}})
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, record), http.StatusOK)
	webtest.WantStatus(t, h.Get(t, h.Env.Priya, record), http.StatusNotFound)
}

// A zero Link id means no link: the form has no chosen row and the read-only
// page shows an empty value, not "0".
func TestScreens_ZeroLinkShowsEmpty(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"stock_uom_id": "0", "name": "Stub"}})

	form := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, form, http.StatusOK)
	if v, ok := form.Value("stock_uom_id"); !ok || v != "" {
		t.Errorf("stock_uom_id value = %q (found %v); want empty", v, ok)
	}

	read := h.Get(t, h.Env.Priya, record)
	webtest.WantStatus(t, read, http.StatusOK)
	var value string
	read.Doc.Find(".summary-row").Each(func(_ int, s *goquery.Selection) {
		if webtest.Squash(s.Find("dt").Text()) == "Stock UoM" {
			value = webtest.Squash(s.Find("dd").Text())
		}
	})
	if value != "" {
		t.Errorf("Stock UoM on the read-only page = %q; want empty", value)
	}
}
