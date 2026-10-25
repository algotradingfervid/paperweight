package files_test

import (
	"bytes"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

func TestSniff_TooLarge(t *testing.T) {
	pdf := filestest.PDF(1024)
	_, err := files.Check(files.DefaultPolicy, "big.pdf", files.MaxFileBytes+1, pdf)
	wantRefused(t, "big.pdf", err, files.KindTooLarge, 413, "File is larger than 10 MB. Choose a smaller file or compress it.")

	_, err = files.Check(files.DefaultPolicy, "max.pdf", files.MaxFileBytes, pdf)
	if err != nil {
		t.Errorf("max.pdf at exactly MaxFileBytes: error = %v, want accepted", err)
	}

	pol := files.DefaultPolicy
	pol.MaxBytes, pol.SizeMessage = 1024, "Logo must be 1 MB or smaller."
	_, err = files.Check(pol, "logo.pdf", 1025, pdf)
	wantRefused(t, "SizeMessage", err, files.KindTooLarge, 413, "Logo must be 1 MB or smaller.")
}

func TestSniff_TypeMessageOverride(t *testing.T) {
	pol := files.DefaultPolicy
	pol.Exts, pol.TypeMessage = []string{".png"}, "Logo must be a PNG or JPG image."
	svg := filestest.SVG()
	_, err := files.Check(pol, "logo.svg", int64(len(svg)), svg)
	wantRefused(t, "logo.svg", err, files.KindType, 422, "Logo must be a PNG or JPG image.")
}

func TestSniff_CSVCutFinalRune(t *testing.T) {
	// Exactly SniffBytes long, ending with the first byte of "é" (C3 A9).
	head := append(bytes.Repeat([]byte("a,b\n"), files.SniffBytes/4-1), "ab,\xC3"...)

	if _, err := files.Check(files.DefaultPolicy, "long.csv", int64(len(head))+100, head); err != nil {
		t.Errorf("long.csv cut by the sample: error = %v, want accepted", err)
	}
	_, err := files.Check(files.DefaultPolicy, "short.csv", int64(len(head)), head)
	wantRefused(t, "short.csv", err, files.KindCSVEncoding, 422, msgCSVEncoding)
}

func TestSniff_RenamedToPNG(t *testing.T) {
	for label, content := range map[string][]byte{"SVG": filestest.SVG(), "HTML": filestest.HTML()} {
		_, err := check("image.png", content)
		wantRefused(t, label+" as .png", err, files.KindMismatch, 422, msgMismatch(".png"))
	}
}

func TestSniff_HeadLongerThanSniffBytes(t *testing.T) {
	// Only the first SniffBytes count: bad UTF-8 after them doesn't refuse the CSV.
	content := append(bytes.Repeat([]byte("a,b\n"), files.SniffBytes/4), "\xC3\x28\n"...)
	_, err := files.Check(files.DefaultPolicy, "big.csv", int64(len(content)), content)
	if err != nil {
		t.Errorf("big.csv: error = %v, want accepted (bytes after SniffBytes are not checked)", err)
	}
}

func TestSniff_NegativeSizeIsEmpty(t *testing.T) {
	_, err := files.Check(files.DefaultPolicy, "neg.pdf", -1, filestest.PDF(1024))
	wantRefused(t, "neg.pdf", err, files.KindEmpty, 422, msgEmpty)
}
