package companyweb_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	companyweb "github.com/vardhan/internaltools/internal/masters/company/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// The logo tests use the real files.Store: filesfake has no LogoPolicy and no
// Spool(io.Reader, int64), and CD-209 gives masters the Store. It is wired as WP3 wires it: LogoPolicy for
// the Company, and companyweb.Info in front of the engine.
const (
	logoPath   = "/m/company/1/logo"
	removePath = logoPath + "/remove"

	msgLogoType = "Logo must be a PNG or JPG image."
	msgLogoSize = "Logo must be 1 MB or smaller."
	msgNoFile   = "Choose a file to upload."

	msgLogoChanged = "The logo changed after you opened this page. Reload it and make your changes again."

	// requestLimit ends a request that waits on a connection it can't get, so
	// a pool test fails on the status, not at the test binary's timeout.
	requestLimit = 10 * time.Second
)

type logoWorld struct {
	*world
	store   *files.Store
	dataDir string
}

func newLogoWorld(t *testing.T) *logoWorld {
	t.Helper()
	lw := &logoWorld{world: &world{}, dataDir: t.TempDir()}
	lw.h = webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		svc := company.New(k)
		lw.svc = svc
		st, err := files.New(files.Config{
			DataDir: lw.dataDir, DB: k.DB, Auth: k.Auth, Info: companyweb.Info(svc, k.Engine), Clock: k.Clock,
			Policies: map[string]files.Policy{company.CompanyDocType: files.LogoPolicy},
		})
		if err != nil {
			t.Fatal(err)
		}
		lw.store = st
		return func(r chi.Router) {
			companyweb.Routes(svc, k)(r)
			companyweb.LogoRoutes(svc, k, st)(r)
		}
	})
	return lw
}

// seeded is newLogoWorld with the fixture company saved.
func seeded(t *testing.T) *logoWorld {
	t.Helper()
	lw := newLogoWorld(t)
	if err := lw.svc.SaveCompany(context.Background(), lw.h.Env.Kavya, company.CompanyInput{LegalName: legalName, PAN: pan}, 0); err != nil {
		t.Fatal(err)
	}
	return lw
}

// part is one multipart part: a file when name is set, else a plain field.
type part struct {
	field, name string
	data        []byte
}

// upload posts one "file" part to target, as the page's form does.
func (lw *logoWorld) upload(t *testing.T, p auth.Principal, target, name string, data []byte, htmx bool) webtest.Response {
	t.Helper()
	var parts []part
	if name != "" {
		parts = []part{{"file", name, data}}
	}
	return lw.send(t, p, target, htmx, parts...)
}

func (lw *logoWorld) send(t *testing.T, p auth.Principal, target string, htmx bool, parts ...part) webtest.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, pt := range parts {
		var w io.Writer
		var err error
		if pt.name != "" {
			w, err = mw.CreateFormFile(pt.field, pt.name)
		} else {
			w, err = mw.CreateFormField(pt.field)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(pt.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	ctx, cancel := context.WithTimeout(auth.WithPrincipal(req.Context(), p), requestLimit)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	lw.h.Router.ServeHTTP(rec, req)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	return webtest.Response{Status: rec.Code, Location: rec.Header().Get("Location"), Header: rec.Header(), Body: rec.Body.String(), Doc: doc}
}

// uploadAction and removeAction are where the rendered page's forms post.
func uploadAction(t *testing.T, page webtest.Response) string {
	t.Helper()
	got, ok := page.Doc.Find(`.upload__controls form`).Attr("action")
	if !ok {
		t.Fatalf("no upload form: %s", webtest.Squash(page.Body))
	}
	return got
}

func removeAction(t *testing.T, page webtest.Response) string {
	t.Helper()
	got, ok := page.Doc.Find(`.attachments form`).Attr("action")
	if !ok {
		t.Fatalf("no Remove form: %s", webtest.Squash(page.Body))
	}
	return got
}

// post uploads through the form on the logo page, as Kavya.
func (lw *logoWorld) post(t *testing.T, name string, data []byte) webtest.Response {
	t.Helper()
	kavya := lw.h.Env.Kavya
	return lw.upload(t, kavya, uploadAction(t, lw.h.Get(t, kavya, logoPath)), name, data, false)
}

func (lw *logoWorld) logos(t *testing.T) []files.Meta {
	t.Helper()
	metas, err := lw.store.List(context.Background(), lw.h.Env.Kavya, auth.RecordRef{DocType: company.CompanyDocType, ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	return metas
}

// logoURL is what the letterhead gets for a request by p.
func (lw *logoWorld) logoURL(t *testing.T, p auth.Principal) string {
	t.Helper()
	var got string
	ctx := auth.WithPrincipal(context.Background(), p)
	if err := lw.h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = companyweb.LogoURL(lw.svc, lw.store)(ctx, tx)
		return err
	}); err != nil {
		t.Fatalf("LogoURL: %v", err)
	}
	return got
}

func fileURL(id int64) string { return "/files/" + strconv.FormatInt(id, 10) }

func TestLogo_UploadShowsItAndFeedsTheLetterhead(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya

	if got := lw.logoURL(t, kavya); got != "" {
		t.Fatalf("LogoURL before any upload = %q; want empty, so the brand logo falls back", got)
	}

	page := lw.h.Get(t, kavya, logoPath)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "PNG or JPG, up to 1 MB")
	webtest.WantNoText(t, page, "Take photo")
	if got, _ := page.Doc.Find(`input[type=file]`).Attr("accept"); got != ".png,.jpg,.jpeg" {
		t.Errorf("accept = %q", got)
	}
	if n := page.Doc.Find(`.attachments`).Length(); n != 0 {
		t.Errorf("page shows a logo before any upload")
	}

	resp := lw.post(t, "logo.png", filestest.PNG(184_320))
	webtest.WantRedirect(t, resp, logoPath)
	webtest.WantBanner(t, lw.h.Follow(t, kavya, resp), "Logo saved.")

	metas := lw.logos(t)
	if len(metas) != 1 || metas[0].Name != "logo.png" {
		t.Fatalf("logos = %+v; want one logo.png", metas)
	}
	if got, want := lw.logoURL(t, kavya), fileURL(metas[0].ID); got != want {
		t.Errorf("LogoURL = %q; want %q", got, want)
	}

	page = lw.h.Get(t, kavya, logoPath)
	webtest.WantText(t, page, "logo.png")
	webtest.WantText(t, page, "180 KB")
	if got, want := removeAction(t, page), removePath+"?version=1&logo="+strconv.FormatInt(metas[0].ID, 10); got != want {
		t.Errorf("Remove form action = %q; want %q", got, want)
	}
	if n := page.Doc.Find(`.attachments img[src="` + fileURL(metas[0].ID) + `"]`).Length(); n != 1 {
		t.Errorf("page shows %d images of the logo; want 1", n)
	}
}

// forBranch is the print's path to the logo: ForBranch in one read tx, with
// the pool at one connection, so a second connection fails on ctx's deadline.
func (lw *logoWorld) forBranch(t *testing.T, ctx context.Context) (string, error) {
	t.Helper()
	b, err := lw.svc.Create(context.Background(), lw.h.Env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27"})
	if err != nil {
		t.Fatal(err)
	}
	defer lw.onePool()()
	ctx, cancel := context.WithTimeout(ctx, requestLimit)
	defer cancel()
	l := company.Letterhead{S: lw.svc, LogoURL: companyweb.LogoURL(lw.svc, lw.store)}
	var got string
	err = lw.h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := l.ForBranch(ctx, tx, b.ID)
		got = info.LogoURL
		return err
	})
	return got, err
}

// onePool leaves the pool one connection and returns what restores it. A tx
// that asks for a second connection then waits on itself until its ctx ends,
// so a call with no deadline must restore the pool first.
func (lw *logoWorld) onePool() func() {
	pool := lw.h.Env.DB.SQL()
	pool.SetMaxOpenConns(1)
	return func() { pool.SetMaxOpenConns(8) }
}

// CD-209, FILE-048: a print by a user with no Company read gets the logo, in
// the print's own tx.
func TestLogo_LetterheadForAnySignedInUser(t *testing.T) {
	lw := seeded(t)
	lw.post(t, "logo.png", filestest.PNG(1024))
	meena := testkit.Principal(t, "meena@vardhan.test")
	ctx := auth.WithPrincipal(context.Background(), meena)
	err := lw.h.Env.Kit.Auth.Can(ctx, meena, company.CompanyDocType, auth.RightRead, &auth.RecordRef{DocType: company.CompanyDocType, ID: 1})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("precondition: Can(meena, Company, read) = %v; want refused", err)
	}
	got, err := lw.forBranch(t, ctx)
	if err != nil {
		t.Fatalf("ForBranch: %v", err)
	}
	if want := fileURL(lw.logos(t)[0].ID); got != want {
		t.Errorf("LetterheadInfo.LogoURL = %q; want %q", got, want)
	}
}

// Only a signed-in user gets the logo (FILE-048): the print's guard always puts
// one on ctx, so none is a refusal, never a silent brand logo.
func TestLogo_LetterheadWithNoPrincipalIsRefused(t *testing.T) {
	lw := seeded(t)
	lw.post(t, "logo.png", filestest.PNG(1024))
	if got, err := lw.forBranch(t, context.Background()); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("ForBranch with no principal = %q, %v; want forbidden", got, err)
	}
}

// The review measured 7 writers stalled 5 s behind a replace that opened a
// second connection under the write lock. With one connection in the pool the
// writers queue for the tx's own connection, so any second connection the
// logo code asks for waits on itself until requestLimit, and fails this test
// on the status, not on a clock. It runs as Administrator; the non-admin cases
// are in logo_nosecondconn_test.go (CD-213).
func TestLogo_ReplaceTakesNoSecondConnectionAsAdministrator(t *testing.T) {
	lw := seeded(t)
	admin := lw.h.Env.Admin
	lw.post(t, "first.png", filestest.PNG(1024))
	action := uploadAction(t, lw.h.Get(t, admin, logoPath))
	restore := lw.onePool()

	ctx, cancel := context.WithTimeout(context.Background(), requestLimit)
	defer cancel()
	const writers = 7
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() { errs <- lw.h.Env.DB.Write(ctx, func(*db.Tx) error { return nil }) })
	}
	resp := lw.upload(t, admin, action, "second.png", filestest.PNG(2048), false)
	wg.Wait()
	restore()
	close(errs)
	webtest.WantRedirect(t, resp, logoPath)
	for err := range errs {
		if err != nil {
			t.Errorf("queued writer: %v", err)
		}
	}
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "second.png" {
		t.Errorf("logos = %+v; want only second.png", got)
	}
}

func TestLogo_HTMXUpload(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya

	// A refusal swaps the upload alone, with the message.
	resp := lw.upload(t, kavya, logoPath+"?version=1", "logo.svg", filestest.SVG(), true)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgLogoType)
	if resp.Doc.Find(`#main`).Length() != 0 || resp.Doc.Find(`#company-logo`).Length() != 1 {
		t.Errorf("htmx refusal is not the upload fragment: %s", webtest.Squash(resp.Body))
	}

	// A swap can't show the flash: the page reloads.
	resp = lw.upload(t, kavya, logoPath+"?version=1", "logo.png", filestest.PNG(1024), true)
	webtest.WantStatus(t, resp, http.StatusOK)
	if got := resp.Header.Get("HX-Redirect"); got != logoPath {
		t.Errorf("HX-Redirect = %q; want %q", got, logoPath)
	}
	if len(lw.logos(t)) != 1 {
		t.Error("htmx upload stored nothing")
	}

	// An error htmx can't swap is not a page.
	resp = lw.upload(t, lw.h.Env.Priya, logoPath+"?version=1", "logo.png", filestest.PNG(1024), true)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	if resp.Doc.Find(`#main`).Length() != 0 {
		t.Errorf("htmx 403 rendered a page: %s", webtest.Squash(resp.Body))
	}
}

// The refusals keep the policy's words on the page (FILE-048), whichever
// layer stops the file, and store nothing.
func TestLogo_Refusals(t *testing.T) {
	lw := seeded(t)
	max := int(files.LogoPolicy.MaxBytes)
	for _, tc := range []struct {
		name, file string
		data       []byte
		msg        string
	}{
		{"wrong type", "logo.svg", filestest.SVG(), msgLogoType},
		{"a PDF", "logo.pdf", filestest.PNG(1024), msgLogoType},
		{"one byte over", "logo.png", filestest.PNG(max + 1), msgLogoSize},
		{"far over", "logo.png", filestest.PNG(max + max/2), msgLogoSize},
		{"no file", "", nil, msgNoFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := lw.post(t, tc.file, tc.data)
			webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
			webtest.WantText(t, resp, tc.msg)
			if n := resp.Doc.Find(`#main .error-summary a[href="#company-logo-file"]`).Length(); n != 1 {
				t.Errorf("ErrorSummary links to the upload %d times; want 1", n)
			}
			if n := resp.Doc.Find(`a[href="/m/company/1"]`).Length(); n == 0 {
				t.Error("refusal page has no Back link to the company")
			}
			if got := lw.logos(t); len(got) != 0 {
				t.Errorf("stored %+v after the refusal", got)
			}
		})
	}
	// One file with a large plain field: only the body limit stops this.
	pad := part{field: "pad", data: bytes.Repeat([]byte("x"), max+int(filesweb.MultipartOverhead)+1)}
	over := lw.send(t, lw.h.Env.Kavya, logoPath+"?version=1", false, pad, part{"file", "logo.png", filestest.PNG(1024)})
	webtest.WantStatus(t, over, http.StatusUnprocessableEntity)
	webtest.WantText(t, over, msgLogoSize)

	two := lw.send(t, lw.h.Env.Kavya, logoPath+"?version=1", false, part{"file", "a.png", filestest.PNG(1024)}, part{"file", "b.png", filestest.PNG(2048)})
	webtest.WantStatus(t, two, http.StatusUnprocessableEntity)
	webtest.WantText(t, two, filesweb.MsgTwoFileParts)
	if got := lw.logos(t); len(got) != 0 {
		t.Fatalf("stored %+v after the refusals", got)
	}

	resp := lw.h.Post(t, lw.h.Env.Kavya, logoPath+"?version=1", url.Values{}) // not multipart
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgNoFile)
	if resp := lw.post(t, "logo.png", filestest.PNG(max)); resp.Status != http.StatusSeeOther {
		t.Fatalf("a logo of exactly the limit = %d; want saved", resp.Status)
	}
}

func TestLogo_NewUploadReplacesTheOld(t *testing.T) {
	lw := seeded(t)
	first, second := filestest.PNG(1024), filestest.PNG(2048)
	lw.post(t, "first.png", first)
	old := lw.logos(t)[0]

	resp := lw.post(t, "second.png", second)
	webtest.WantRedirect(t, resp, logoPath)
	got := lw.logos(t)
	if len(got) != 1 || got[0].Name != "second.png" || got[0].ID == old.ID {
		t.Fatalf("logos after replace = %+v; want only second.png", got)
	}

	// The same bytes again aren't a duplicate of what they replace.
	lw.post(t, "again.png", second)
	if got = lw.logos(t); len(got) != 1 || got[0].Name != "again.png" {
		t.Fatalf("logos after the same bytes = %+v; want only again.png", got)
	}

	// files/web's generic route can add a second attachment: a replace still
	// leaves one logo.
	extra := filestest.PNG(4096)
	sp, err := lw.store.Spool(bytes.NewReader(extra), files.MaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	ctx := auth.WithPrincipal(context.Background(), lw.h.Env.Kavya)
	if err := lw.h.Env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := lw.store.Attach(ctx, tx, lw.h.Env.Kavya, auth.RecordRef{DocType: company.CompanyDocType, ID: 1}, "extra.png", sp)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	lw.post(t, "only.png", filestest.PNG(8192))
	if got = lw.logos(t); len(got) != 1 || got[0].Name != "only.png" {
		t.Fatalf("logos after replacing two = %+v; want only only.png", got)
	}

	// A refused replacement rolls the removal back.
	resp = lw.post(t, "logo.svg", filestest.SVG())
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	if after := lw.logos(t); len(after) != 1 || after[0].ID != got[0].ID {
		t.Fatalf("logos after a refused replacement = %+v; want %+v kept", after, got[0])
	}
}

func TestLogo_Remove(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya
	lw.post(t, "logo.png", filestest.PNG(1024))

	resp := lw.h.Post(t, kavya, removeAction(t, lw.h.Get(t, kavya, logoPath)), url.Values{})
	webtest.WantRedirect(t, resp, logoPath)
	webtest.WantBanner(t, lw.h.Follow(t, kavya, resp), "Logo removed.")
	if got := lw.logos(t); len(got) != 0 {
		t.Fatalf("logos after remove = %+v", got)
	}
	if got := lw.logoURL(t, kavya); got != "" {
		t.Errorf("LogoURL after remove = %q; want empty", got)
	}
}

// A Remove rendered before a rival's upload must not take the rival's logo.
func TestLogo_RemoveOfAReplacedLogoIsRefused(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya
	lw.post(t, "mine.png", filestest.PNG(1024))
	action := removeAction(t, lw.h.Get(t, kavya, logoPath))
	lw.post(t, "rival.png", filestest.PNG(2048))

	resp := lw.h.Post(t, kavya, action, url.Values{})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, msgLogoChanged)
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "rival.png" {
		t.Fatalf("logos = %+v; want rival.png kept", got)
	}

	// Gone altogether is the same refusal.
	lw.h.Post(t, kavya, removeAction(t, lw.h.Get(t, kavya, logoPath)), url.Values{})
	webtest.WantStatus(t, lw.h.Post(t, kavya, action, url.Values{}), http.StatusConflict)

	webtest.WantStatus(t, lw.h.Post(t, kavya, removePath+"?version=1", url.Values{}), http.StatusBadRequest)
}

func TestLogo_ReadOnlyIsForbidden(t *testing.T) {
	lw := seeded(t)
	kavya, priya := lw.h.Env.Kavya, lw.h.Env.Priya
	lw.post(t, "logo.png", filestest.PNG(1024))
	link := `a[href="` + logoPath + `"]`

	if n := lw.h.Get(t, kavya, "/m/company/1").Doc.Find(link).Length(); n != 1 {
		t.Errorf("Kavya's company page has %d links to the logo page; want 1", n)
	}
	page := lw.h.Get(t, priya, "/m/company/1")
	webtest.WantStatus(t, page, http.StatusOK)
	if n := page.Doc.Find(link).Length(); n != 0 {
		t.Errorf("Priya (read only) sees %d links to the logo page", n)
	}

	want := deny(auth.RightWrite, "Company")
	for name, resp := range map[string]webtest.Response{
		"page":   lw.h.Get(t, priya, logoPath),
		"upload": lw.upload(t, priya, logoPath+"?version=1", "new.png", filestest.PNG(2048), false),
		"remove": lw.h.Post(t, priya, removePath+"?version=1&logo=1", url.Values{}),
	} {
		if resp.Status != http.StatusForbidden || !strings.Contains(resp.Text(), want) {
			t.Errorf("%s = %d %q; want 403 %q", name, resp.Status, resp.Text(), want)
		}
		if resp.Doc.Find(`#main`).Length() != 1 {
			t.Errorf("%s: the 403 is not a page", name)
		}
	}
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "logo.png" {
		t.Errorf("logos after Priya's refused writes = %+v", got)
	}
}

// CD-223: own scope follows the company's created_by on the page, the link, the
// upload and files.Store's own check, which reads it through Info.
func TestLogo_OwnScopeFollowsTheCreator(t *testing.T) {
	lw := newLogoWorld(t)
	ctx := context.Background()
	env := lw.h.Env
	a := env.Kit.Auth.(*auth.Auth)
	const role = "Own Company"
	if err := a.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRights(ctx, env.Admin, role, company.CompanyDocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
	}); err != nil {
		t.Fatal(err)
	}
	own := auth.Principal{ID: 7, Name: "Own", Roles: []string{role}}
	other := auth.Principal{ID: 3, Name: "Other", Roles: []string{role}}
	if err := lw.svc.SaveCompany(ctx, own, company.CompanyInput{LegalName: legalName, PAN: pan}, 0); err != nil {
		t.Fatal(err)
	}

	if n := lw.h.Get(t, own, "/m/company/1").Doc.Find(`a[href="` + logoPath + `"]`).Length(); n != 1 {
		t.Errorf("the owner's page has %d links to the logo page; want 1", n)
	}
	webtest.WantStatus(t, lw.h.Get(t, own, logoPath), http.StatusOK)
	webtest.WantRedirect(t, lw.upload(t, own, logoPath+"?version=1", "logo.png", filestest.PNG(1024), false), logoPath)
	if got := lw.logos(t); len(got) != 1 {
		t.Fatalf("logos = %+v", got)
	}

	webtest.WantStatus(t, lw.h.Get(t, other, logoPath), http.StatusNotFound)
	webtest.WantStatus(t, lw.upload(t, other, logoPath+"?version=1", "new.png", filestest.PNG(2048), false), http.StatusNotFound)
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "logo.png" {
		t.Errorf("logos after a refused upload = %+v", got)
	}
}

// The company's version guards both writes; attachments have none of their
// own, so a logo change does not move it.
func TestLogo_StaleVersion(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya
	lw.post(t, "logo.png", filestest.PNG(1024))
	if got := lw.company(t).Version; got != 1 {
		t.Fatalf("company version after a logo upload = %d; want 1", got)
	}
	if err := lw.svc.SaveCompany(context.Background(), kavya, company.CompanyInput{LegalName: newName, PAN: pan}, 1); err != nil {
		t.Fatal(err)
	}

	page := lw.h.Get(t, kavya, logoPath)
	if got, want := uploadAction(t, page), logoPath+"?version=2"; got != want {
		t.Fatalf("upload action = %q; want %q", got, want)
	}
	staleRemove := strings.Replace(removeAction(t, page), "version=2", "version=1", 1)

	resp := lw.upload(t, kavya, logoPath+"?version=1", "new.png", filestest.PNG(2048), false)
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleCompany)
	if got, want := uploadAction(t, resp), logoPath+"?version=1"; got != want {
		t.Errorf("redrawn upload action = %q; want the posted version, %q", got, want)
	}
	webtest.WantBanner(t, resp, staleCompany)
	if got, _ := resp.Doc.Find(`.banner a`).Attr("href"); got != logoPath {
		t.Errorf("409 Reload link = %q; want %q", got, logoPath)
	}
	tmp, err := os.ReadDir(filepath.Join(lw.dataDir, "files", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmp) != 0 {
		t.Errorf("%d spooled files left after the 409", len(tmp))
	}
	resp = lw.h.Post(t, kavya, staleRemove, url.Values{})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleCompany)
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "logo.png" {
		t.Errorf("logos after stale writes = %+v; want logo.png kept", got)
	}

	// htmx swaps the same banner, and the area it swaps holds it, so a later
	// swap replaces it. Its Reload is the way out: the fresh page's form saves.
	resp = lw.upload(t, kavya, logoPath+"?version=1", "new.png", filestest.PNG(2048), true)
	webtest.WantStatus(t, resp, http.StatusConflict)
	if n := resp.Doc.Find(`#company-logo-area .banner`).Length(); n != 1 {
		t.Errorf("htmx 409 has %d banners in the swapped area: %s", n, webtest.Squash(resp.Body))
	}
	if got, _ := resp.Doc.Find(`#company-logo form`).First().Attr("hx-target"); got != "#company-logo-area" {
		t.Errorf("upload form hx-target = %q; want #company-logo-area", got)
	}
	reload, _ := resp.Doc.Find(`.banner a`).Attr("href")
	fresh := lw.h.Get(t, kavya, reload)
	webtest.WantStatus(t, fresh, http.StatusOK)
	webtest.WantRedirect(t, lw.upload(t, kavya, uploadAction(t, fresh), "new.png", filestest.PNG(2048), false), logoPath)
	if got := lw.logos(t); len(got) != 1 || got[0].Name != "new.png" {
		t.Errorf("logos after Reload = %+v; want new.png", got)
	}

	for _, target := range []string{logoPath, logoPath + "?version=x", removePath} {
		var resp webtest.Response
		if target == removePath {
			resp = lw.h.Post(t, kavya, target, url.Values{})
		} else {
			resp = lw.upload(t, kavya, target, "new.png", filestest.PNG(2048), false)
		}
		webtest.WantStatus(t, resp, http.StatusBadRequest)
		webtest.WantText(t, resp, auth.ErrBadForm.Error())
	}
}

func TestLogo_OnlyCompanyOneAndOnlyAfterSetup(t *testing.T) {
	lw := newLogoWorld(t)
	kavya := lw.h.Env.Kavya

	if got := lw.logoURL(t, kavya); got != "" {
		t.Errorf("LogoURL with no company = %q; want empty", got)
	}
	missing := lw.h.Get(t, kavya, logoPath)
	webtest.WantStatus(t, missing, http.StatusNotFound)
	if missing.Doc.Find(`#main`).Length() != 1 {
		t.Error("the 404 is not a page")
	}
	resp := lw.upload(t, kavya, logoPath+"?version=0", "logo.png", filestest.PNG(1024), false)
	webtest.WantStatus(t, resp, http.StatusNotFound)

	if err := lw.svc.SaveCompany(context.Background(), kavya, company.CompanyInput{LegalName: legalName, PAN: pan}, 0); err != nil {
		t.Fatal(err)
	}
	webtest.WantStatus(t, lw.h.Get(t, kavya, "/m/company/2/logo"), http.StatusNotFound)
}

// CD-201(2): only id 1 is the company.
func TestScreens_CompanyOtherIDsAreNotFound(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	webtest.WantStatus(t, w.h.Get(t, kavya, "/m/company/2"), http.StatusNotFound)
	resp := w.h.Post(t, kavya, "/m/company/2", url.Values{"version": {"1"}, "name": {newName}, "pan": {pan}})
	webtest.WantStatus(t, resp, http.StatusNotFound)
	if got := w.company(t); got.LegalName != legalName || got.Version != 1 {
		t.Errorf("POST /m/company/2 changed the company: %+v", got)
	}
}

// Version 0 is SaveCompany's create path. A posted one is stale like any other
// version that is not the company's: 409 with the company, 404 without.
func TestScreens_CompanyVersionBelowOneIsStale(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	for _, v := range []string{"0", "-1"} {
		resp := w.h.Post(t, kavya, "/m/company/1", url.Values{"version": {v}, "name": {newName}, "pan": {pan}})
		webtest.WantStatus(t, resp, http.StatusConflict)
		webtest.WantText(t, resp, strings.ReplaceAll(staleCompany, newName, legalName))
	}
	if got := w.company(t); got.LegalName != legalName || got.Version != 1 {
		t.Errorf("company after version 0 = %+v; want unchanged", got)
	}

	// Before setup, a posted 0 must not create a company with no branch.
	lw := newLogoWorld(t)
	resp := lw.h.Post(t, lw.h.Env.Kavya, "/m/company/1", url.Values{"version": {"0"}, "name": {newName}, "pan": {pan}})
	webtest.WantStatus(t, resp, http.StatusNotFound)
	lw.read(t, func(ctx context.Context, tx *db.Tx) error {
		if _, ok, err := lw.svc.Company(ctx, tx); err != nil || ok {
			t.Errorf("Company after a posted 0 before setup: ok %v, err %v; want none", ok, err)
		}
		return nil
	})
}

// Check fields go both ways: "1" or "0" to the form, present or absent back.
func TestScreens_BranchHeadOfficeBothWays(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	box := `input[name="is_head_office"]`
	pune, blr := w.mustBranch(t, "PUNE"), w.mustBranch(t, "BLR")
	if _, on := w.h.Get(t, kavya, branchPath(pune.ID, "")).Doc.Find(box).Attr("checked"); !on {
		t.Error("PUNE's head office box is not checked")
	}
	if _, on := w.h.Get(t, kavya, branchPath(blr.ID, "")).Doc.Find(box).Attr("checked"); on {
		t.Error("BLR's head office box is checked")
	}

	form := url.Values{"version": {ver(pune.Version)}, "code": {"PUNE"}, "name": {pune.Name}, "state_code": {"27"}, "gstin": {pune.GSTIN}}
	webtest.WantRedirect(t, w.h.Post(t, kavya, branchPath(pune.ID, ""), form), branchPath(pune.ID, ""))
	pune = w.mustBranch(t, "PUNE")
	if pune.IsHeadOffice {
		t.Error("PUNE is still head office with the box off")
	}
	form.Set("version", ver(pune.Version))
	form.Set("is_head_office", "1")
	webtest.WantRedirect(t, w.h.Post(t, kavya, branchPath(pune.ID, ""), form), branchPath(pune.ID, ""))
	if !w.mustBranch(t, "PUNE").IsHeadOffice {
		t.Error("PUNE is not head office with the box on")
	}
}

// MST-019: nothing creates, disables or deletes the company.
func TestScreens_CompanyHidesItsMissingActions(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	list := w.h.Get(t, kavya, "/m/company")
	webtest.WantText(t, list, "1 company")
	webtest.WantNoText(t, list, "New")
	webtest.WantNoText(t, list, "Show disabled")
	page := w.h.Get(t, kavya, "/m/company/1")
	webtest.WantNoText(t, page, "Disable")
	webtest.WantNoText(t, page, "Delete")
	for _, target := range []string{"/m/company/new", "/m/company/1/disable", "/m/company/1/enable"} {
		if resp := w.h.Post(t, kavya, target, url.Values{"version": {"1"}}); resp.Status < 400 {
			t.Errorf("POST %s = %d; want a refusal", target, resp.Status)
		}
	}
}

func TestScreens_BranchCopyAndCodes(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	webtest.WantText(t, w.h.Get(t, kavya, "/m/branch"), "2 branches")
	page := w.h.Get(t, kavya, "/m/branch/new")
	if n := page.Doc.Find(`[name="gstin"][hx-post], [name="gstin"][hx-get]`).Length(); n != 1 {
		t.Errorf("gstin is not a CodeInput: %d checked controls", n)
	}
	co := w.h.Get(t, kavya, "/m/company/1")
	if n := co.Doc.Find(`[name="pan"][hx-post], [name="pan"][hx-get]`).Length(); n != 1 {
		t.Errorf("pan is not a CodeInput: %d checked controls", n)
	}
	for _, tc := range []struct{ path, want string }{
		{"/m/branch", "New branch"},
		{"/m/branch?q=zzz", "No results"},
	} {
		webtest.WantText(t, w.h.Get(t, kavya, tc.path), tc.want)
	}
}

func TestInfo_AnswersTheCompanyAndPassesTheRestOn(t *testing.T) {
	w := newWorld(t)
	info := companyweb.Info(w.svc, w.h.Env.Kit.Engine)
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		got, err := info.Info(ctx, tx, company.CompanyDocType, 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.DocLabel != "Company" || got.ID != 1 || got.Display != legalName || got.State != "" || got.CreatedBy != w.h.Env.Kavya.ID {
			t.Errorf("Info(Company, 1) = %+v", got)
		}
		if _, err := info.Info(ctx, tx, company.CompanyDocType, 2); !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("Info(Company, 2) = %v; want not found", err)
		}
		if _, err := info.Info(ctx, tx, company.DocType, 1); !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("Info(Branch, 1) = %v; want the engine's not found", err)
		}
		return nil
	})
}
