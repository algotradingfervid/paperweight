package print_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const testPO = "test.PurchaseOrder"

// poRegistry has one view for test.PurchaseOrder that renders only the title block.
func poRegistry(t *testing.T) *print.Registry {
	t.Helper()
	reg := print.NewRegistry()
	reg.Register(testPO, func(context.Context, *db.Tx, int64) (templ.Component, error) {
		return print.DocTitle("Purchase Order"), nil
	})
	return reg
}

func renderPO(t *testing.T, state doctype.State, id int64, number, display string) string {
	t.Helper()
	return printtest.RenderString(t, poRegistry(t), doctype.Info{DocType: testPO, ID: id, State: state, Number: number, Display: display})
}

// FILE-061
func TestPrint_TitleIsSafeNumber(t *testing.T) {
	cases := []struct {
		state           doctype.State
		number, display string
		want            string
	}{
		{doctype.StateApproved, "PO/26-27/00001", "PO/26-27/00001", "<title>PO_26-27_00001</title>"},
		{doctype.StateApproved, "PO/26-27/00001-1", "PO/26-27/00001-1", "<title>PO_26-27_00001-1</title>"},
		{doctype.StateDraft, "", "Draft 42", "<title>Draft 42</title>"},
	}
	for _, c := range cases {
		html := renderPO(t, c.state, 42, c.number, c.display)
		if !strings.Contains(html, c.want) {
			t.Errorf("render %q: missing %s in\n%s", c.display, c.want, html)
		}
	}
	if got := print.SafeTitle(`a\b%c?d#e`); got != "a_b_c_d_e" {
		t.Errorf(`SafeTitle(a\b%%c?d#e) = %q, want "a_b_c_d_e"`, got)
	}
	if got := print.SafeTitle("PO/26-27/00001"); got != "PO_26-27_00001" {
		t.Errorf("SafeTitle(PO/26-27/00001) = %q", got)
	}
}

// Supporting test (no case ID; feeds WP3's FILE-056, CD-77).
// TestPrintCSS_ClosingStaysTogether: totals, words, terms, approval and the
// signatory move to the next page as one block (FILE-057).
func TestPrintCSS_ClosingStaysTogether(t *testing.T) {
	for _, want := range []string{
		".totals, .words, .terms, .approved { break-inside: avoid; break-after: avoid; }",
		".closing { break-inside: avoid; }",
	} {
		if !strings.Contains(print.CSS, want) {
			t.Errorf("print.CSS missing %q", want)
		}
	}
}

func TestPrintCSS_A4(t *testing.T) {
	for _, want := range []string{
		"@page { size: A4 portrait; margin: 15mm; }",
		".watermark { position: fixed;",
		"transform: rotate(-30deg)",
		"pointer-events: none",
		"thead { display: table-header-group; }",
		"tr { break-inside: avoid; }",
		".closing { break-inside: avoid; }",
		"img { image-orientation: from-image; }",
		"var(--doc-accent)",
		".logo { max-inline-size: 60mm; max-block-size: 20mm; inline-size: auto; block-size: auto; object-fit: contain }",
	} {
		if !strings.Contains(print.CSS, want) {
			t.Errorf("print.CSS missing %q", want)
		}
	}
}

// Supporting test (no case ID): the page shell parts the plan fixes.
func TestPrint_PageShell(t *testing.T) {
	html := renderPO(t, doctype.StateDraft, 42, "", "Draft 42")
	for _, want := range []string{
		"<!doctype html>",
		`<html lang="en">`,
		`<link rel="stylesheet" href="/print/assets/print.css">`,
		`<link rel="stylesheet" href="/print/assets/brand.css">`,
		`<body><div class="watermark" data-status="draft">DRAFT</div>`,
		`<h1 class="doc-title">Purchase Order`,
		`<p class="doc-number">Draft 42</p>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q in\n%s", want, html)
		}
	}
	for _, bad := range []string{"<style", "style="} {
		if strings.Contains(html, bad) {
			t.Errorf("page contains %q (CSP forbids it):\n%s", bad, html)
		}
	}
	for _, tag := range strings.Split(html, "<script")[1:] {
		if end := strings.IndexByte(tag, '>'); end < 0 || !strings.Contains(tag[:end], "src=") {
			t.Errorf("page contains an inline <script (CSP forbids it):\n%s", html)
		}
	}
}

func TestPrint_RenderUnknownDocType(t *testing.T) {
	var sb strings.Builder
	err := print.NewRegistry().Render(t.Context(), nil, &sb, doctype.Info{DocType: "test.None", ID: 1, Display: "Draft 1"})
	if !errors.Is(err, print.ErrNoTemplate) {
		t.Fatalf("Render unknown DocType err = %v, want ErrNoTemplate", err)
	}
}
