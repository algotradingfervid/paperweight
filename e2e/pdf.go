//go:build e2e

package e2e

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// PDFPage is one printed page: its size in points and the text it draws, in
// drawing order, one chunk per text-showing operator.
type PDFPage struct {
	Width, Height float64
	Chunks        []string
}

// Text is the page's text with its spaces gone: Chrome draws most glyphs one by
// one and leaves spaces out, so a word boundary can't be recovered.
func (p PDFPage) Text() string {
	return strings.Join(strings.Fields(strings.Join(p.Chunks, "")), "")
}

// pdfObject is one indirect object: its dictionary and its inflated stream.
type pdfObject struct {
	dict   string
	stream []byte
}

var (
	pdfObjectStart = regexp.MustCompile(`(?m)^(\d+) 0 obj\n`)
	pdfLength      = regexp.MustCompile(`/Length (\d+)`)
	pdfMediaBox    = regexp.MustCompile(`/MediaBox \[0 0 ([0-9.]+) ([0-9.]+)\]`)
	pdfContents    = regexp.MustCompile(`/Contents (\d+) 0 R`)
	pdfFontRef     = regexp.MustCompile(`/(F\d+) (\d+) 0 R`)
	pdfToUnicode   = regexp.MustCompile(`/ToUnicode (\d+) 0 R`)
	pdfBfChar      = regexp.MustCompile(`(?m)^<([0-9A-Fa-f]+)> <([0-9A-Fa-f]+)>$`)
	pdfBfRange     = regexp.MustCompile(`(?m)^<([0-9A-Fa-f]+)> <([0-9A-Fa-f]+)> <([0-9A-Fa-f]+)>$`)
	pdfTextOp      = regexp.MustCompile(`/(F\d+) [0-9.]+ Tf|<([0-9A-Fa-f]*)> Tj|\[((?:<[0-9A-Fa-f]*>|[-0-9. ]+)*)\] TJ`)
	pdfHexString   = regexp.MustCompile(`<([0-9A-Fa-f]*)>`)
)

// ParsePDF reads what Chrome's print writes: no object streams, Flate content
// streams and a ToUnicode map on every font. It is enough to see which text
// landed on which page, with no PDF library in go.mod.
func ParsePDF(t *testing.T, data []byte) []PDFPage {
	t.Helper()
	objects := map[int]pdfObject{}
	var numbers []int
	starts := pdfObjectStart.FindAllSubmatchIndex(data, -1)
	for i, m := range starts {
		n, err := strconv.Atoi(string(data[m[2]:m[3]]))
		if err != nil {
			t.Fatal(err)
		}
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		objects[n] = pdfParseObject(data[m[1]:end])
		numbers = append(numbers, n)
	}
	var pages []PDFPage
	for _, n := range numbers {
		o := objects[n]
		if !strings.Contains(o.dict, "/Type /Page\n") {
			continue
		}
		box := pdfMediaBox.FindStringSubmatch(o.dict)
		contents := pdfContents.FindStringSubmatch(o.dict)
		if box == nil || contents == nil {
			t.Fatalf("page object %d has no media box or contents: %s", n, o.dict)
		}
		w, err := strconv.ParseFloat(box[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		h, err := strconv.ParseFloat(box[2], 64)
		if err != nil {
			t.Fatal(err)
		}
		fonts := map[string]map[string]string{}
		for _, f := range pdfFontRef.FindAllStringSubmatch(o.dict, -1) {
			fonts[f[1]] = pdfFontMap(t, objects, f[2])
		}
		id, err := strconv.Atoi(contents[1])
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, PDFPage{Width: w, Height: h, Chunks: pdfChunks(objects[id].stream, fonts)})
	}
	if len(pages) == 0 {
		t.Fatal("no pages in the PDF")
	}
	return pages
}

// pdfParseObject splits an object's body into dictionary and stream. A stream
// that doesn't inflate (an image, say) stays empty.
func pdfParseObject(body []byte) pdfObject {
	before, after, ok := bytes.Cut(body, []byte("stream\n"))
	if !ok {
		return pdfObject{dict: string(body)}
	}
	o := pdfObject{dict: string(before)}
	m := pdfLength.FindSubmatch(before)
	if m == nil {
		return o
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil || n > len(after) {
		return o
	}
	r, err := zlib.NewReader(bytes.NewReader(after[:n]))
	if err != nil {
		return o
	}
	o.stream, _ = io.ReadAll(r) // what inflated is all a damaged stream gives
	return o
}

// pdfFontMap reads font object ref's ToUnicode map: hex code to text.
func pdfFontMap(t *testing.T, objects map[int]pdfObject, ref string) map[string]string {
	t.Helper()
	n, err := strconv.Atoi(ref)
	if err != nil {
		t.Fatal(err)
	}
	m := pdfToUnicode.FindStringSubmatch(objects[n].dict)
	if m == nil {
		return nil
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	cmap := string(objects[id].stream)
	out := map[string]string{}
	for _, c := range pdfBfChar.FindAllStringSubmatch(cmap, -1) {
		out[strings.ToLower(c[1])] = pdfUTF16(c[2])
	}
	for _, c := range pdfBfRange.FindAllStringSubmatch(cmap, -1) {
		lo, err := strconv.ParseInt(c[1], 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		hi, err := strconv.ParseInt(c[2], 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		dst, err := strconv.ParseInt(c[3], 16, 32)
		if err != nil {
			t.Fatal(err)
		}
		for v := lo; v <= hi; v++ {
			key := strconv.FormatInt(v, 16)
			key = strings.Repeat("0", len(c[1])-len(key)) + key
			out[strings.ToLower(key)] = pdfUTF16(fmt.Sprintf("%04x", dst+v-lo))
		}
	}
	return out
}

// pdfUTF16 decodes a big-endian UTF-16 hex string, as a ToUnicode map holds.
func pdfUTF16(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil {
		return ""
	}
	var r []rune
	for i := 0; i+1 < len(b); i += 2 {
		r = append(r, rune(b[i])<<8|rune(b[i+1]))
	}
	return string(r)
}

// pdfDecode turns a hex string drawn in font into text. The code width is the
// width of the font's own map keys.
func pdfDecode(h string, font map[string]string) string {
	width := 2
	for k := range font {
		width = len(k)
		break
	}
	var b strings.Builder
	for i := 0; i+width <= len(h); i += width {
		b.WriteString(font[strings.ToLower(h[i:i+width])])
	}
	return b.String()
}

// pdfChunks lists the text a content stream draws, in order.
func pdfChunks(content []byte, fonts map[string]map[string]string) []string {
	var out []string
	var font map[string]string
	for _, op := range pdfTextOp.FindAllSubmatch(content, -1) {
		switch {
		case len(op[1]) > 0:
			font = fonts[string(op[1])]
		case len(op[3]) > 0:
			var s strings.Builder
			for _, h := range pdfHexString.FindAllSubmatch(op[3], -1) {
				s.WriteString(pdfDecode(string(h[1]), font))
			}
			out = append(out, s.String())
		default:
			out = append(out, pdfDecode(string(op[2]), font))
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return strings.TrimSpace(s) == "" })
}
