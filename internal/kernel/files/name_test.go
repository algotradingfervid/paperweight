package files_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/files"
)

// FILE-012
func TestFileName_Sanitize(t *testing.T) {
	long := strings.Repeat("x", 300) + ".pdf"
	for _, tc := range []struct{ in, want string }{
		{"../../etc/passwd.pdf", "passwd.pdf"},
		{`C:\temp\bill.pdf`, "bill.pdf"},
		{"a/b%?#.pdf", "b___.pdf"},
		{"  .bill.pdf. ", "bill.pdf"},
		{"bi\x00ll\x1f.pdf", "bill.pdf"},
		{".pdf", "file.pdf"},
		{"RECEIPT.JPG", "RECEIPT.JPG"},
		{"बिल रसीद.jpg", "बिल रसीद.jpg"},
		{"bi\u0085ll\u009f.pdf", "bill.pdf"},
		{"invoice\u202efdp.exe", "invoicefdp.exe"},
		{"\u2066a\u200eb\u200fc\u061cd\u202a\u2069.pdf", "abcd.pdf"},
		{"a." + strings.Repeat("y", 300), "a." + strings.Repeat("y", 118)},
		{strings.Repeat("x", 114) + " .zzzz.pdf", strings.Repeat("x", 114) + ".pdf"},
	} {
		if got := files.SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	got := files.SanitizeName(long)
	if utf8.RuneCountInString(got) != files.MaxNameRunes || !strings.HasSuffix(got, ".pdf") {
		t.Errorf("long name = %d runes %q", utf8.RuneCountInString(got), got)
	}
	if files.Ext("Bill.Pdf") != ".pdf" || files.Ext("README") != "" || files.Ext("bill.exe.pdf") != ".pdf" {
		t.Error("Ext")
	}
}
