package files

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// MaxNameRunes is the longest display name kept, extension included.
const MaxNameRunes = 120

// bidiControls are dropped so a name can't display reordered, e.g. "invoice\u202efdp.exe".
const bidiControls = "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"

// SanitizeName turns an uploaded file name into a safe display name (FILE-012).
// Storage never uses it; blobs live at BlobPath.
func SanitizeName(raw string) string {
	name := raw[strings.LastIndexAny(raw, `/\`)+1:]
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20 || (r >= 0x7f && r <= 0x9f), strings.ContainsRune(bidiControls, r):
			return -1
		case strings.ContainsRune(`%?#";:*<>|`, r):
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, " .")

	base, ext := name, ""
	// An extension that leaves no room for a base isn't kept as one (CD-109).
	if i := strings.LastIndex(name, "."); i >= 0 && utf8.RuneCountInString(name[i:]) < MaxNameRunes {
		base, ext = name[:i], name[i:]
	}
	base = strings.TrimLeft(base, " .")
	if base == "" {
		base = "file"
	}
	if r := []rune(base); len(r)+utf8.RuneCountInString(ext) > MaxNameRunes {
		base = strings.TrimRight(string(r[:MaxNameRunes-utf8.RuneCountInString(ext)]), " .")
	}
	return base + ext
}

// Ext returns the lower-case extension of name with its dot, or "" when it has none.
func Ext(name string) string {
	return strings.ToLower(filepath.Ext(name))
}
