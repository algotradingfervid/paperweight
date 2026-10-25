package files

import (
	"bytes"
	"fmt"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
)

// Checked is an upload that passed Check.
type Checked struct {
	Name, Ext, ContentType string
}

// heifTypes are the detected types a .heic or .heif file may have; the detected one is stored.
var heifTypes = []string{"image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence"}

// csvTypes are the detected types a .csv file may have before the text rules apply.
var csvTypes = []string{"text/plain", "text/csv", "text/tab-separated-values", "application/octet-stream"}

// Check accepts an upload only when its extension is in pol.Exts and its content
// matches that extension (FILE-005 to FILE-011). head is the first min(size, SniffBytes) bytes.
func Check(pol Policy, rawName string, size int64, head []byte) (Checked, error) {
	head = head[:min(len(head), SniffBytes)]
	name := SanitizeName(rawName)
	ext := Ext(name)
	if !slices.Contains(pol.Exts, ext) {
		shown := ext
		if shown == "" {
			shown = "(none)"
		}
		msg := fmt.Sprintf(msgType, shown)
		if pol.TypeMessage != "" {
			msg = pol.TypeMessage
		}
		return Checked{}, &Error{Kind: KindType, Code: http.StatusUnprocessableEntity, Message: msg}
	}
	if size <= 0 {
		return Checked{}, &Error{Kind: KindEmpty, Code: http.StatusUnprocessableEntity, Message: msgEmpty}
	}
	if size > pol.MaxBytes {
		msg := msgTooLarge
		if pol.SizeMessage != "" {
			msg = pol.SizeMessage
		}
		return Checked{}, &Error{Kind: KindTooLarge, Code: http.StatusRequestEntityTooLarge, Message: msg}
	}

	mt := mimetype.Detect(head)
	ct := ""
	switch ext {
	case ".pdf":
		ct = matching(mt, "application/pdf")
	case ".jpg", ".jpeg":
		ct = matching(mt, "image/jpeg")
	case ".png":
		ct = matching(mt, "image/png")
	case ".webp":
		ct = matching(mt, "image/webp")
	case ".heic", ".heif":
		ct = matching(mt, heifTypes...)
	case ".xlsx":
		ct = matching(mt, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	case ".docx":
		ct = matching(mt, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".csv":
		if matching(mt, csvTypes...) != "" && bytes.IndexByte(head, 0) < 0 {
			if !csvText(head, int64(len(head)) < size) {
				return Checked{}, &Error{Kind: KindCSVEncoding, Code: http.StatusUnprocessableEntity, Message: msgCSVEncoding}
			}
			ct = "text/csv"
		}
	}
	if ct == "" {
		return Checked{}, &Error{Kind: KindMismatch, Code: http.StatusUnprocessableEntity, Message: fmt.Sprintf(msgMismatch, ext)}
	}
	return Checked{Name: name, Ext: ext, ContentType: ct}, nil
}

// matching returns the first of types that mt is, without parameters, or "".
func matching(mt *mimetype.MIME, types ...string) string {
	for _, t := range types {
		if mt.Is(t) {
			return t
		}
	}
	return ""
}

// csvText reports whether head, without a UTF-8 BOM, is valid UTF-8. When head
// is cut short of the file, a final rune it cuts off doesn't count.
func csvText(head []byte, cut bool) bool {
	head = bytes.TrimPrefix(head, []byte("\xEF\xBB\xBF"))
	if cut {
		for i := len(head) - 1; i >= 0 && i >= len(head)-utf8.UTFMax; i-- {
			if utf8.RuneStart(head[i]) {
				if !utf8.FullRune(head[i:]) {
					head = head[:i]
				}
				break
			}
		}
	}
	return utf8.Valid(head)
}
