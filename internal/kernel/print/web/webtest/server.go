// Package webtest is a test server over the real print and files handlers,
// for WP3's browser tests (CD-40).
package webtest

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	platformweb "github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// Seeded records.
const (
	ReferencePOID int64 = 1  // approved PO/26-27/00001, printtest.TableDocType
	DraftPOID     int64 = 42 // Draft 42 with DraftLines lines
	DraftLines          = 60 // every third has a description long enough to wrap to two lines

	CompanyDocType       = "company"
	CompanyID      int64 = 1
	ExpenseDocType       = "expense"
	ExpenseID      int64 = 1

	LogoID    int64 = 1 // logo.png on the company, the letterhead's /files/1
	ReceiptID int64 = 2 // r.jpg on the expense
	HEICID    int64 = 3 // r.heic on the expense
	EXIF6ID   int64 = 4 // r-rotated.jpg on the expense, EXIF orientation 6
)

// NewServer starts a server that serves print/web.Routes and files/web.Routes
// under platform/web.NewRouter, with every request signed in as as. as may
// read, write and print every seeded record; the server closes when t ends.
func NewServer(t testing.TB, as auth.Principal) *httptest.Server {
	t.Helper()
	e := filestest.New(t)
	for _, dt := range []string{printtest.TableDocType, CompanyDocType, ExpenseDocType} {
		e.Auth.Allow(as.ID, dt, "read", "write", "print")
	}
	seedPOs(t, e.DB)
	company := e.Doc(CompanyDocType, CompanyID, "active", "Vardhan Infra Projects Pvt Ltd", as.ID)
	expense := e.Doc(ExpenseDocType, ExpenseID, "draft", "Draft 1", as.ID)
	exif := filestest.JPEGWithEXIF(6)
	for _, a := range []struct {
		parent auth.RecordRef
		name   string
		body   []byte
	}{
		{company, "logo.png", logoPNG(t)},
		{expense, "r.jpg", photoJPEG(t)},
		{expense, "r.heic", filestest.HEIC("heic")},
		// The EXIF segment goes after SOI, before the real image's segments.
		{expense, "r-rotated.jpg", append(exif[:len(exif)-2:len(exif)-2], photoJPEG(t)[2:]...)},
	} {
		if _, err := e.Attach(as, a.parent, a.name, a.body); err != nil {
			t.Fatalf("webtest: attach %s: %v", a.name, err)
		}
	}

	reg := print.NewRegistry()
	reg.Register(printtest.TableDocType, view)
	pr := printweb.Routes(printweb.Config{
		DB: e.DB, Auth: e.Auth, Info: printtest.TableInfo{}, Letterhead: printtest.Letterhead, Registry: reg,
	})
	fr := filesweb.Routes(filesweb.Config{Store: e.Store, DB: e.DB})

	r := platformweb.NewRouter(platformweb.Options{Auth: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), as)))
		})
	}})
	// Handle, not Mount: both sub-routers match the full path.
	r.Handle("/print/*", pr)
	r.Handle("/files/*", fr)
	r.Handle("/attachments/*", fr)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// seedPOs creates print_test_po with the reference PO and Draft 42.
func seedPOs(t testing.TB, d *db.DB) {
	t.Helper()
	printtest.CreateTable(t, d)
	err := d.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `
INSERT INTO print_test_po (id, state, number, party_name, party_address, version, updated_at) VALUES
	(?, 'approved', 'PO/26-27/00001', 'Shree Steel Traders', 'Mumbai, Maharashtra', 1, '2026-10-24T05:00:00.000Z'),
	(?, 'draft', '', 'Shree Steel Traders', 'Mumbai, Maharashtra', 1, '2026-10-24T05:00:00.000Z')`,
			ReferencePOID, DraftPOID)
		return err
	})
	if err != nil {
		t.Fatalf("webtest: seed POs: %v", err)
	}
}

// view prints Draft 42 as the reference PO's lines repeated to DraftLines
// rows (taxable 2,394,000.00, CGST and SGST 215,460.00 each, total
// 2,824,920.00), and every other id through printtest.TableView.
func view(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	if id != DraftPOID {
		return printtest.TableView(ctx, tx, id)
	}
	lh, err := print.LetterheadFor(ctx, tx, printtest.PuneID)
	if err != nil {
		return nil, err
	}
	d := printtest.ReferencePO()
	d.Letterhead = lh
	ref := d.Lines
	d.Lines = nil
	d.Taxable = 0
	for i := range DraftLines {
		l := ref[i%len(ref)]
		l.Sr = i + 1
		if i%3 == 2 {
			l.Description = "Cut to 6 m lengths, primer coated and delivered to the PUNE stores bay 3 with mill test certificates"
		}
		d.Lines = append(d.Lines, l)
		d.Taxable += l.Amount
	}
	// Both GSTINs are in Maharashtra: keep the reference PO's CGST and SGST
	// rows, recomputed on the larger taxable value.
	d.GrandTotal = d.Taxable
	for i := range d.Taxes {
		d.Taxes[i].Amount = d.Taxable * types.Money(d.Taxes[i].Rate) / types.Money(types.MaxPercent)
		d.GrandTotal += d.Taxes[i].Amount
	}
	d.RoundedTotal = d.GrandTotal
	return print.TradeDocument(d), nil
}

// logoPNG is a 120×40 single-colour PNG.
func logoPNG(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(120, 40)); err != nil {
		t.Fatalf("webtest: logo: %v", err)
	}
	return buf.Bytes()
}

// photoJPEG is a 64×32 single-colour JPEG, wider than tall so a rotation shows.
func photoJPEG(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, solid(64, 32), nil); err != nil {
		t.Fatalf("webtest: photo: %v", err)
	}
	return buf.Bytes()
}

func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: 0x1f, G: 0x4e, B: 0x8c, A: 0xff})
		}
	}
	return img
}
