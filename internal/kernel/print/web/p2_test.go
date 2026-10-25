package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Supporting test (no case ID; feeds WP3's FILE-058 and FILE-070): the page
// footer stylesheet and print.js are served, with the page's permission checks.
func TestPrint_ToolbarAndFooterServed(t *testing.T) {
	fa := authfake.New()
	seedRights(fa)
	rv := ravi(t, fa)
	priya := testkit.Principal(t, "priya@vardhan.test")
	fa.AddRole(priya.ID, "Purchase User")
	fa.Restrict(priya.ID, []int64{prjMetro}, nil)

	df := docfake.New()
	df.SetInfo(approvedPO1())
	df.SetInfo(doctype.Info{DocType: poType, DocLabel: "Purchase Order", ID: 42, State: doctype.StateDraft, Display: "Draft 42"})
	df.SetInfo(doctype.Info{DocType: poType, DocLabel: "Purchase Order", ID: 43, State: doctype.StateDraft, Display: `a"b\c`})
	df.SetInfo(doctype.Info{DocType: poType, DocLabel: "Purchase Order", ID: 44, State: doctype.StateDraft, Display: "x\ny"})
	df.SetInfo(doctype.Info{DocType: poType, DocLabel: "Purchase Order", ID: 45, State: doctype.StateDraft, Display: "p\fq\rr"})
	df.SetInfo(doctype.Info{DocType: "test.Other", DocLabel: "Other", ID: 46, State: doctype.StateDraft, Display: "Other 46"})
	fa.Grant("Purchase Manager", "test.Other", "print", auth.ScopeAll)
	reg := print.NewRegistry()
	reg.Register(poType, titleView)
	h := web.Routes(web.Config{DB: testkit.NewDB(t), Auth: fa, Info: df, Letterhead: printtest.Letterhead, Registry: reg})

	for target, want := range map[string]string{
		"/print/test.PurchaseOrder/42/page.css": `@page { @bottom-center { content: "Draft 42 · Page " counter(page) " of " counter(pages); } }`,
		"/print/test.PurchaseOrder/1/page.css":  `@page { @bottom-center { content: "PO/26-27/00001 · Page " counter(page) " of " counter(pages); } }`,
		"/print/test.PurchaseOrder/43/page.css": `content: "a\"b\\c · Page "`,
		"/print/test.PurchaseOrder/44/page.css": `content: "x\a y · Page "`,
		"/print/test.PurchaseOrder/45/page.css": `content: "p\c q\d r · Page "`,
	} {
		rec := get(t, h, rv, target)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("GET %s: got %d %q, want 200 containing %q", target, rec.Code, rec.Body.String(), want)
		}
		if strings.ContainsAny(rec.Body.String(), "\n\r\f") {
			t.Errorf("GET %s: body %q has a raw LF, CR or FF", target, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
			t.Errorf("GET %s Content-Type = %q", target, got)
		}
	}

	rec := get(t, h, rv, "/print/test.Other/46/page.css")
	if rec.Code != http.StatusNotFound || rec.Body.String() != "No print template for DocType test.Other.\n" {
		t.Errorf("page.css with no view: got %d %q, want 404 for the missing template", rec.Code, rec.Body.String())
	}

	rec = get(t, h, priya, "/print/test.PurchaseOrder/1/page.css")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), notFoundMsg) {
		t.Errorf("Priya (restricted to PRJ-METRO) page.css: got %d %q, want 404 containing %q", rec.Code, rec.Body.String(), notFoundMsg)
	}

	rec = get(t, h, rv, "/print/assets/print.js")
	if rec.Code != http.StatusOK || rec.Body.String() != print.JS || print.JS == "" {
		t.Errorf("print.js: got %d, body is not print.JS", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("print.js Content-Type = %q", got)
	}
}
