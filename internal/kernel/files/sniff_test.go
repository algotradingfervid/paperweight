package files_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

const (
	msgEmpty       = "File is empty. Choose another file."
	msgCSVEncoding = "CSV files must be UTF-8. Save the file as CSV UTF-8 and try again."
)

func msgType(ext string) string {
	return "File type " + ext + " is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
}

func msgMismatch(ext string) string {
	return "File content does not match its " + ext + " extension. Upload the original file."
}

// check runs files.Check with the default policy, passing the whole content as head.
func check(name string, content []byte) (files.Checked, error) {
	return files.Check(files.DefaultPolicy, name, int64(len(content)), content)
}

func wantAccepted(t *testing.T, name string, content []byte, wantName, wantExt, wantType string) {
	t.Helper()
	got, err := check(name, content)
	if err != nil {
		t.Errorf("Check(%q) error = %v, want accepted", name, err)
		return
	}
	if got.ContentType != wantType {
		t.Errorf("Check(%q).ContentType = %q, want %q", name, got.ContentType, wantType)
	}
	if got.Name != wantName {
		t.Errorf("Check(%q).Name = %q, want %q", name, got.Name, wantName)
	}
	if got.Ext != wantExt {
		t.Errorf("Check(%q).Ext = %q, want %q", name, got.Ext, wantExt)
	}
}

func wantRefused(t *testing.T, label string, err error, kind files.Kind, status int, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted, want refused with %q", label, msg)
		return
	}
	if k := files.KindOf(err); k != kind {
		t.Errorf("%s: KindOf = %v, want %v", label, k, kind)
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", label, err.Error(), msg)
	}
	var fe *files.Error
	if !errors.As(err, &fe) {
		t.Errorf("%s: error %T is not *files.Error", label, err)
	} else if fe.Code != status {
		t.Errorf("%s: Code = %d, want %d", label, fe.Code, status)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Errorf("%s: error %T does not implement doctype.HTTPError (CD-44)", label, err)
	} else {
		if he.Status() != status {
			t.Errorf("%s: Status() = %d, want %d", label, he.Status(), status)
		}
		if fes := he.FieldErrors(); fes != nil {
			t.Errorf("%s: FieldErrors() = %v, want nil", label, fes)
		}
	}
}

// wantMagic guards the content builders: real magic bytes at off.
func wantMagic(t *testing.T, label string, b []byte, off int, magic string) {
	t.Helper()
	if len(b) < off+len(magic) || string(b[off:off+len(magic)]) != magic {
		t.Fatalf("fixture %s: want %q at offset %d, got % x", label, magic, off, b[:min(len(b), off+len(magic)+4)])
	}
}

func wantLen(t *testing.T, label string, b []byte, n int) {
	t.Helper()
	if len(b) != n {
		t.Fatalf("fixture %s: len = %d, want padded to %d", label, len(b), n)
	}
}

func wantOOXML(t *testing.T, label string, b []byte, dir string) {
	t.Helper()
	wantMagic(t, label, b, 0, "PK\x03\x04")
	if !bytes.Contains(b, []byte("[Content_Types].xml")) || !bytes.Contains(b, []byte(dir)) {
		t.Fatalf("fixture %s: want a ZIP with [Content_Types].xml and %s entries", label, dir)
	}
}

// FILE-006
func TestSniff_AllowedTypes(t *testing.T) {
	pdf, jpg, png, webp := filestest.PDF(1024), filestest.JPEG(1024), filestest.PNG(1024), filestest.WEBP(1024)
	heic, heif := filestest.HEIC("heic"), filestest.HEIC("mif1")
	xlsx, docx := filestest.XLSX(), filestest.DOCX()
	csv := []byte("code,name\nBOLT-M12,Hex Bolt\n")

	wantLen(t, "PDF", pdf, 1024)
	wantMagic(t, "PDF", pdf, 0, "%PDF-")
	wantLen(t, "JPEG", jpg, 1024)
	wantMagic(t, "JPEG", jpg, 0, "\xFF\xD8\xFF")
	wantLen(t, "PNG", png, 1024)
	wantMagic(t, "PNG", png, 0, "\x89PNG\r\n\x1a\n")
	wantLen(t, "WEBP", webp, 1024)
	wantMagic(t, "WEBP", webp, 0, "RIFF")
	wantMagic(t, "WEBP", webp, 8, "WEBP")
	wantMagic(t, "HEIC(heic)", heic, 4, "ftypheic")
	wantMagic(t, "HEIC(mif1)", heif, 4, "ftypmif1")
	wantOOXML(t, "XLSX", xlsx, "xl/")
	wantOOXML(t, "DOCX", docx, "word/")

	for _, tc := range []struct {
		name    string
		content []byte
		ext     string
		want    string
	}{
		{"bill.pdf", pdf, ".pdf", "application/pdf"},
		{"r.jpg", jpg, ".jpg", "image/jpeg"},
		{"r.jpeg", jpg, ".jpeg", "image/jpeg"},
		{"r.png", png, ".png", "image/png"},
		{"r.webp", webp, ".webp", "image/webp"},
		{"r.heic", heic, ".heic", "image/heic"},
		{"r.heif", heif, ".heif", "image/heif"},
		{"rates.xlsx", xlsx, ".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"quote.docx", docx, ".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"items.csv", csv, ".csv", "text/csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantAccepted(t, tc.name, tc.content, tc.name, tc.ext, tc.want)
		})
	}
}

// FILE-007
func TestAttachment_DisallowedExtensions(t *testing.T) {
	pdf := filestest.PDF(1024)
	for _, tc := range []struct{ name, ext string }{
		{"setup.exe", ".exe"},
		{"page.html", ".html"},
		{"logo.svg", ".svg"},
		{"a.js", ".js"},
		{"docs.zip", ".zip"},
		{"x.phtml", ".phtml"},
		{"old.xls", ".xls"},
		{"README", "(none)"},
	} {
		_, err := check(tc.name, pdf)
		wantRefused(t, tc.name, err, files.KindType, 422, msgType(tc.ext))
	}
}

// FILE-008
func TestAttachment_ExtensionCaseInsensitive(t *testing.T) {
	wantAccepted(t, "RECEIPT.JPG", filestest.JPEG(1024), "RECEIPT.JPG", ".jpg", "image/jpeg")
	wantAccepted(t, "Bill.Pdf", filestest.PDF(1024), "Bill.Pdf", ".pdf", "application/pdf")
}

// FILE-009
func TestSniff_ContentExtensionMismatch(t *testing.T) {
	html, png, pe := filestest.HTML(), filestest.PNG(100), filestest.PE()
	if !strings.HasPrefix(strings.ToLower(string(html)), "<!doctype html><script>") {
		t.Fatalf("fixture HTML: want it to start <!doctype html><script>, got %q", html[:min(len(html), 30)])
	}
	wantLen(t, "PNG", png, 100)
	wantMagic(t, "PNG", png, 0, "\x89PNG\r\n\x1a\n")
	wantMagic(t, "PE", pe, 0, "MZ")

	for _, tc := range []struct {
		name    string
		content []byte
		ext     string
	}{
		{"invoice.pdf", html, ".pdf"},
		{"photo.jpg", png, ".jpg"},
		{"items.csv", pe, ".csv"},
	} {
		_, err := check(tc.name, tc.content)
		wantRefused(t, tc.name, err, files.KindMismatch, 422, msgMismatch(tc.ext))
	}
	wantAccepted(t, "photo.jpeg", filestest.JPEG(1024), "photo.jpeg", ".jpeg", "image/jpeg")
}

// FILE-010
func TestSniff_CSVTextRules(t *testing.T) {
	_, err := check("a.csv", []byte("<html><body>x</body></html>"))
	wantRefused(t, "a.csv", err, files.KindMismatch, 422, msgMismatch(".csv"))

	_, err = check("b.csv", []byte("code,name\n\xC3\x28\n"))
	wantRefused(t, "b.csv", err, files.KindCSVEncoding, 422, msgCSVEncoding)

	wantAccepted(t, "c.csv", []byte("\xEF\xBB\xBFcode,name\nA,B\n"), "c.csv", ".csv", "text/csv")
}

// FILE-011
func TestAttachment_DoubleExtension(t *testing.T) {
	pdf := filestest.PDF(1024)
	_, err := check("bill.pdf.exe", pdf)
	wantRefused(t, "bill.pdf.exe", err, files.KindType, 422, msgType(".exe"))

	wantAccepted(t, "bill.exe.pdf", pdf, "bill.exe.pdf", ".pdf", "application/pdf")
}

// FILE-005
func TestAttachment_EmptyFileRejected(t *testing.T) {
	_, err := files.Check(files.DefaultPolicy, "blank.pdf", 0, nil)
	wantRefused(t, "blank.pdf", err, files.KindEmpty, 422, msgEmpty)
}

// Review Focus 5: phones write either brand; the stored type is the detected one.
func TestSniff_HEICBrandVariants(t *testing.T) {
	heicMif1, heifHeic := filestest.HEIC("mif1"), filestest.HEIC("heic")
	wantMagic(t, "HEIC(mif1)", heicMif1, 4, "ftypmif1")
	wantMagic(t, "HEIC(heic)", heifHeic, 4, "ftypheic")

	wantAccepted(t, "IMG_0001.heic", heicMif1, "IMG_0001.heic", ".heic", "image/heif")
	wantAccepted(t, "IMG_0002.heif", heifHeic, "IMG_0002.heif", ".heif", "image/heic")
}
