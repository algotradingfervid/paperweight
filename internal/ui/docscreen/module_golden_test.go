package docscreen_test

import (
	"bytes"
	"flag"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// HOOK-001's golden pages: every docscreen page of a DocType with no Module,
// as base e55f56e rendered it (before MOD-0 Task 1). This file names no MOD-0
// identifier, so it compiles on the base. To add a page, capture it there,
// in a scratch copy outside the worktree, and copy only the new files back:
//
//	git archive e55f56e | tar -x -C /tmp/<scratch>
//	cp internal/ui/docscreen/module_golden_test.go /tmp/<scratch>/internal/ui/docscreen/
//	(cd /tmp/<scratch> && go test ./internal/ui/docscreen/ -run 'TestModule_NoneGolden$' -update-module-golden)
//
// Never run it with the flag on Task 1's code or later: the files are "today".

var updateModuleGolden = flag.Bool("update-module-golden", false, "rewrite testdata/module_none/ from the current code")

const moduleGoldenDir = "testdata/module_none"

// goldenPage is one request whose response is pinned.
type goldenPage struct {
	name   string // the golden file's base name
	setup  func(e *docEnv)
	method string
	url    string
	form   url.Values // POST only; post adds the csrf token and version 3
	hdr    []string
}

// goldenRights is allRights plus create, for the new-document page.
var goldenRights = append([]auth.Right{"create"}, allRights...)

const ship60 = "/doc/" + shipType + "/60"

func seedShip(e *docEnv) {
	docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1", "deliver_to": "Gate 1"}})
}

func seedState(s doctype.State) func(e *docEnv) { return func(e *docEnv) { e.seed(s) } }

// goldenPages lists the pages: the list, the new page, every wizard step of
// the sample (details, items, check) and of the stepped Shipment (details,
// delivery, check), each as a full page and as an htmx frame, a step's 422
// redraw, the line pages, the document page in every state (a draft's
// redirects to its wizard), and every reason page with a 422 redraw.
func goldenPages() []goldenPage {
	draft := seedState(doctype.StateDraft)
	list := func(e *docEnv) { e.d.Lister.(*docscreentest.Lister).Set(docscreentest.Sample, twoRows(2)) }
	hx := []string{"HX-Request", "true"}
	return append([]goldenPage{
		{name: "list", setup: list, url: samplePath},
		{name: "list-empty", url: samplePath + "?q=zz"},
		{name: "new", url: samplePath + "/new"},
		{name: "new-422", method: http.MethodPost, url: samplePath + "/new", form: url.Values{"supplier_id": {"7"}, "order_date": {"31-02-2026"}}},
		{name: "step-details", setup: draft, url: doc42 + "/edit/details"},
		{name: "step-details-htmx", setup: draft, url: doc42 + "/edit/details", hdr: hx},
		{name: "step-details-422", setup: draft, method: http.MethodPost, url: doc42 + "/edit/details", form: url.Values{"supplier_id": {"7"}, "order_date": {"31-02-2026"}, "priority": {"Urgent"}, "remarks": {"typed"}}},
		{name: "step-items", setup: draft, url: doc42 + "/edit/items"},
		{name: "step-items-htmx", setup: draft, url: doc42 + "/edit/items", hdr: hx},
		{name: "step-check", setup: draft, url: doc42 + "/edit/check"},
		{name: "step-check-htmx", setup: draft, url: doc42 + "/edit/check", hdr: hx},
		{name: "ship-details", setup: seedShip, url: ship60 + "/edit/details"},
		{name: "ship-delivery", setup: seedShip, url: ship60 + "/edit/delivery"},
		{name: "ship-delivery-htmx", setup: seedShip, url: ship60 + "/edit/delivery", hdr: hx},
		{name: "ship-check", setup: seedShip, url: ship60 + "/edit/check"},
		{name: "line-1", setup: draft, url: doc42 + "/lines/1"},
		{name: "line-new", setup: draft, url: doc42 + "/lines/new"},
		{name: "line-1-422", setup: draft, method: http.MethodPost, url: doc42 + "/lines/1", form: url.Values{"item_id": {"3"}, "qty": {"x"}, "rate": {"62.5"}}},
		{name: "line-1-remove", setup: draft, url: doc42 + "/lines/1/remove"},
		{name: "page-draft-redirect", setup: draft, url: doc42},
		{name: "page-pending", setup: seedState(doctype.StatePending), url: doc42},
		{name: "page-approved", setup: seedState(doctype.StateApproved), url: doc42},
		{name: "page-rejected", setup: seedState(doctype.StateRejected), url: doc42},
		{name: "page-cancelled", setup: seedState(doctype.StateCancelled), url: doc42},
		{name: "page-closed", setup: seedState(doctype.StateClosed), url: doc42},
		{name: "ship-page-redirect", setup: seedShip, url: ship60},
		{name: "reason-reject", setup: seedState(doctype.StatePending), url: doc42 + "/reject"},
		{name: "reason-reject-422", setup: seedState(doctype.StatePending), method: http.MethodPost, url: doc42 + "/reject", form: url.Values{"reason": {""}}},
		{name: "reason-cancel", setup: seedState(doctype.StateApproved), url: doc42 + "/cancel"},
		{name: "reason-close", setup: seedState(doctype.StateApproved), url: doc42 + "/close"},
		{name: "reason-delete", setup: draft, url: doc42 + "/delete"},
	}, postedPages()...)
}

const (
	goldenConflict = "Someone else changed this document. Reload to see their changes."
	goldenWarning  = "Rate is above the last price."
)

// goldenResult makes the fake answer action a on id with r and err.
func goldenResult(id int64, a doctype.Action, r doc.Result, err error) func(e *docEnv) {
	return func(e *docEnv) { e.docs.SetResult(docscreentest.Sample, id, a, r, err) }
}

// goldenSetup runs each setup in order.
func goldenSetup(fs ...func(e *docEnv)) func(e *docEnv) {
	return func(e *docEnv) {
		for _, f := range fs {
			f(e)
		}
	}
}

// moving applies a successful action's state to the fake (stateEngine), so
// the page an action leads to sees it.
func moving(e *docEnv) { e.d.Engine = stateEngine{e.docs} }

// twoLines seeds draft 42 with a second line, so line 1 can move down.
func twoLines(e *docEnv) {
	rec := sampleRecord()
	rec.Lines = append(rec.Lines, map[string]string{"id": "2", "item_id": "3", "qty": "5.000", "uom": "kg", "rate": "60.0000"})
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
}

// postedPages are the responses to POSTs that render a page: a step save that
// moves on (advance), a step and a new page's 409, a submit's 422 on the
// failing step (failingStep), the check page an htmx withdraw or revise leads
// to (checkPage), a refused remove or move (itemsRefused), a refused action
// on the document page (refused) and the document page an htmx submit or
// approve leads to (succeeded). Each comes as a full page and as htmx.
func postedPages() []goldenPage {
	draft, pending := seedState(doctype.StateDraft), seedState(doctype.StatePending)
	valid := url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}, "priority": {"Normal"}, "remarks": {"typed"}}
	saved := doc.Result{ID: 42, State: doctype.StateDraft, Version: 4, Warnings: []string{goldenWarning}}
	stale := statusErr{status: http.StatusConflict, msg: goldenConflict}
	submit422 := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and submit again.",
		fields: []doctype.FieldError{{Field: "remarks", Message: "Remarks must say where to deliver."}}}
	move422 := statusErr{status: http.StatusUnprocessableEntity, msg: "Check the lines.",
		fields: []doctype.FieldError{{Field: "qty", Line: 2, Message: "Line 2: Quantity must be more than 0."}}}
	approve403 := statusErr{status: http.StatusForbidden, msg: "You can't approve this document."}
	approve422 := statusErr{status: http.StatusUnprocessableEntity, msg: "Check the document.",
		fields: []doctype.FieldError{{Field: "amount", Message: "Amount must be more than 0."}}}
	var out []goldenPage
	both := func(name string, setup func(e *docEnv), u string, form url.Values) {
		out = append(out,
			goldenPage{name: name, setup: setup, method: http.MethodPost, url: u, form: form},
			goldenPage{name: name + "-htmx", setup: setup, method: http.MethodPost, url: u, form: form, hdr: []string{"HX-Request", "true"}})
	}
	both("post-details-advance", goldenSetup(draft, goldenResult(42, doctype.ActionSave, saved, nil)), doc42+"/edit/details", valid)
	both("post-details-409", goldenSetup(draft, goldenResult(42, doctype.ActionSave, doc.Result{}, stale)), doc42+"/edit/details", valid)
	both("post-new-advance", goldenSetup(draft, goldenResult(0, doctype.ActionSave, saved, nil)), samplePath+"/new", valid)
	both("post-new-409", goldenResult(0, doctype.ActionSave, doc.Result{}, stale), samplePath+"/new", valid)
	both("post-submit-422", goldenSetup(draft, goldenResult(42, doctype.ActionSubmit, doc.Result{}, submit422)), doc42+"/submit", nil)
	both("post-withdraw", goldenSetup(pending, moving, goldenResult(42, doctype.ActionWithdraw, saved, nil)), doc42+"/withdraw", nil)
	both("post-revise", goldenSetup(seedState(doctype.StateRejected), moving, goldenResult(42, doctype.ActionRevise, saved, nil)), doc42+"/revise", nil)
	both("post-remove-409", goldenSetup(draft, goldenResult(42, doctype.ActionSave, doc.Result{}, stale)), doc42+"/lines/1/remove", nil)
	both("post-move-409", goldenSetup(twoLines, goldenResult(42, doctype.ActionSave, doc.Result{}, stale)), doc42+"/lines/1/move?dir=down", nil)
	both("post-move-422", goldenSetup(twoLines, goldenResult(42, doctype.ActionSave, doc.Result{}, move422)), doc42+"/lines/1/move?dir=down", nil)
	both("post-approve-403", goldenSetup(pending, goldenResult(42, doctype.ActionApprove, doc.Result{}, approve403)), doc42+"/approve", nil)
	both("post-approve-422", goldenSetup(pending, goldenResult(42, doctype.ActionApprove, doc.Result{}, approve422)), doc42+"/approve", nil)
	both("post-approve", goldenSetup(pending, moving, goldenResult(42, doctype.ActionApprove,
		doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4, Warnings: []string{goldenWarning}}, nil)), doc42+"/approve", nil)
	both("post-submit", goldenSetup(draft, moving, goldenResult(42, doctype.ActionSubmit,
		doc.Result{ID: 42, State: doctype.StatePending, Version: 4}, nil)), doc42+"/submit", nil)
	return out
}

// assetName matches a hashed static asset name (/static/ui/app.<hash>.css
// and the like). Any change under internal/ui changes the CSS hash
// (CLAUDE.md), so the hash is the one normalised part of a page. The CSRF
// token is the harness's constant tok-13, so it needs none.
var assetName = regexp.MustCompile(`(/static/[A-Za-z0-9_./-]+?)\.[0-9a-f]{8}\.(css|js|png|svg|woff2)\b`)

// renderGolden is p's response on a fresh harness: the status, the
// redirect and htmx headers, then the body with asset hashes normalised.
// change, when set, edits the Deps before the router is built.
func renderGolden(t *testing.T, p goldenPage, change func(d *docscreen.Deps)) []byte {
	t.Helper()
	e := newFormEnv(t, goldenRights...)
	if p.setup != nil {
		p.setup(e)
	}
	if change != nil {
		change(&e.d)
	}
	e.remount()
	method := p.method
	if method == "" {
		method = http.MethodGet
	}
	var rec *httptest.ResponseRecorder
	if method == http.MethodPost {
		rec = e.post(t, p.url, maps.Clone(p.form), p.hdr...) // post adds the token and version
	} else {
		rec = e.get(t, p.url, p.hdr...)
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s %s\nstatus: %d\n", method, p.url, rec.Code)
	for _, k := range []string{"Content-Type", "Location", "HX-Redirect", "HX-Push-Url", "HX-Retarget", "HX-Reswap", "HX-Trigger"} {
		if v := rec.Header().Get(k); v != "" {
			fmt.Fprintf(&out, "%s: %s\n", k, v)
		}
	}
	out.WriteString("\n")
	out.Write(assetName.ReplaceAll(rec.Body.Bytes(), []byte("$1.HASH.$2")))
	return out.Bytes()
}

func goldenPath(name string) string { return filepath.Join(moduleGoldenDir, name+".golden") }

// TestModule_NoneGolden writes the golden files with -update-module-golden
// and otherwise checks that the pages, with the Deps the harness builds,
// still match them.
func TestModule_NoneGolden(t *testing.T) {
	if *updateModuleGolden {
		if err := os.MkdirAll(moduleGoldenDir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range goldenPages() {
		t.Run(p.name, func(t *testing.T) {
			got := renderGolden(t, p, nil)
			if *updateModuleGolden {
				if err := os.WriteFile(goldenPath(p.name), got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			wantGolden(t, p.name, got)
		})
	}
}

// wantGolden fails unless got equals the golden file name, byte for byte,
// and names the first differing line.
func wantGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("reading golden %s: %v (capture it on base e55f56e)", name, err)
	}
	if bytes.Equal(got, want) {
		return
	}
	gl, wl := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := range max(len(gl), len(wl)) {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Fatalf("%s differs from today's page at line %d:\n got: %.300s\nwant: %.300s", name, i+1, g, w)
		}
	}
	t.Fatalf("%s differs from today's page", name)
}
