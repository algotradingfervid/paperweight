package naming

import (
	"fmt"
	"strings"
)

// part is one piece of a template. Exactly one field is set: lit for literal
// text, tok (FY|YYYY|YY|MM) for a date token, digits (> 0) for the number part.
type part struct {
	lit    string
	tok    string
	digits int
}

// template is a parsed, valid series template.
type template struct {
	src    string
	parts  []part
	digits int
}

// tokenLen is the rendered width of each date token.
var tokenLen = map[string]int{"FY": 5, "YYYY": 4, "YY": 2, "MM": 2}

func invalidTemplate(msg string) error {
	return &Error{Kind: ErrInvalidTemplate, Msg: msg, Field: "template"}
}

func isAlnum(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// parseTemplate parses s and checks the rules in order, returning the first
// that fails as an *Error with Kind ErrInvalidTemplate.
func parseTemplate(s string) (template, error) {
	// Rule 1: split into literals and brace bodies; braces balanced, not nested.
	type piece struct {
		text  string
		brace bool
	}
	var pieces []piece
	var cur strings.Builder
	inBrace := false
	for _, r := range s {
		switch r {
		case '{':
			if inBrace {
				return template{}, invalidTemplate(msgBraces)
			}
			if cur.Len() > 0 {
				pieces = append(pieces, piece{text: cur.String()})
				cur.Reset()
			}
			inBrace = true
		case '}':
			if !inBrace {
				return template{}, invalidTemplate(msgBraces)
			}
			pieces = append(pieces, piece{text: cur.String(), brace: true})
			cur.Reset()
			inBrace = false
		default:
			cur.WriteRune(r)
		}
	}
	if inBrace {
		return template{}, invalidTemplate(msgBraces)
	}
	if cur.Len() > 0 {
		pieces = append(pieces, piece{text: cur.String()})
	}

	// Rule 2: literal characters.
	for _, p := range pieces {
		if p.brace {
			continue
		}
		for _, r := range p.text {
			if !isAlnum(r) && r != '-' && r != '/' {
				return template{}, invalidTemplate(msgBadChars)
			}
		}
	}

	// Rule 3: brace bodies.
	t := template{src: s}
	for _, p := range pieces {
		switch {
		case !p.brace:
			t.parts = append(t.parts, part{lit: p.text})
		case tokenLen[p.text] > 0:
			t.parts = append(t.parts, part{tok: p.text})
		case p.text != "" && strings.Trim(p.text, "#") == "":
			t.parts = append(t.parts, part{digits: len(p.text)})
		default:
			return template{}, invalidTemplate(fmt.Sprintf(msgUnknownToken, "{"+p.text+"}"))
		}
	}

	// Rule 4: exactly one number part.
	numbers, at := 0, -1
	for i, p := range t.parts {
		if p.digits > 0 {
			numbers++
			at = i
			t.digits = p.digits
		}
	}
	switch {
	case numbers == 0:
		return template{}, invalidTemplate(msgMissingNumber)
	case numbers > 1:
		return template{}, invalidTemplate(msgTwoNumbers)
	}

	// Rule 5: padding.
	if t.digits < 3 || t.digits > 6 {
		return template{}, invalidTemplate(msgPadding)
	}

	// Rule 6: number part last.
	if at != len(t.parts)-1 {
		return template{}, invalidTemplate(msgNumberNotLast)
	}

	// Rule 7: first character; tokens always render digits.
	if first := t.parts[0]; first.lit != "" && !isAlnum(rune(first.lit[0])) {
		return template{}, invalidTemplate(msgFirstChar)
	}

	// Rule 8: length.
	if n := t.maxLen(); n > 14 {
		return template{}, invalidTemplate(fmt.Sprintf(msgTooLong, n))
	}
	return t, nil
}

// staticPrefix is the source text before the number part, e.g. "PO/{FY}/".
//
//nolint:unused // used by series and admin checks from Task 6 on
func (t template) staticPrefix() string {
	var b strings.Builder
	for _, p := range t.parts {
		switch {
		case p.lit != "":
			b.WriteString(p.lit)
		case p.tok != "":
			b.WriteString("{" + p.tok + "}")
		}
	}
	return b.String()
}

// maxLen is the longest rendered base number.
func (t template) maxLen() int {
	n := 0
	for _, p := range t.parts {
		n += len(p.lit) + tokenLen[p.tok] + p.digits
	}
	return n
}
