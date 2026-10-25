// Package filestest holds test helpers for the files package: sample contents with real magic bytes.
package filestest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
)

// pad appends zero bytes to b until it is n long.
func pad(b []byte, n int) []byte {
	if len(b) < n {
		b = append(b, make([]byte, n-len(b))...)
	}
	return b
}

// PDF returns n bytes that start like a PDF.
func PDF(n int) []byte { return pad([]byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"), n) }

// JPEG returns n bytes that start like a JPEG (SOI, JFIF APP0).
func JPEG(n int) []byte {
	return pad([]byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"), n)
}

// PNG returns n bytes that start like a PNG (signature, IHDR header).
func PNG(n int) []byte { return pad([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), n) }

// WEBP returns n bytes that start like a WEBP (RIFF container, VP8 chunk).
func WEBP(n int) []byte {
	b := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	b = pad(b, n)
	binary.LittleEndian.PutUint32(b[4:8], uint32(max(len(b)-8, 0))) //nolint:gosec // test sizes are small
	return b
}

// HEIC returns an ISO-BMFF ftyp box with the given major brand, as phones write
// for HEIC/HEIF photos ("heic", "mif1", …).
func HEIC(brand string) []byte {
	b := []byte("\x00\x00\x00\x18ftyp")
	b = append(b, brand...)
	b = append(b, "\x00\x00\x00\x00mif1heic"...)
	return b
}

// XLSX returns a minimal workbook ZIP in Excel's entry order.
func XLSX() []byte {
	return ooxml("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`)
}

// DOCX returns a minimal document ZIP in Word's entry order.
func DOCX() []byte {
	return ooxml("word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`)
}

func ooxml(part, body string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, body string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{part, body},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// HTML returns an HTML page with a script.
func HTML() []byte {
	return []byte("<!doctype html><script>alert(1)</script><p>invoice</p></html>")
}

// PE returns the start of a Windows executable (MZ header pointing at a PE header).
func PE() []byte {
	b := pad([]byte("MZ"), 0x40)
	binary.LittleEndian.PutUint32(b[0x3C:], 0x40)
	return pad(append(b, "PE\x00\x00"...), 256)
}

// SVG returns a small SVG image.
func SVG() []byte {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`)
}

// JPEGWithEXIF returns a JPEG whose EXIF APP1 segment carries the given orientation tag.
func JPEGWithEXIF(orientation int) []byte {
	tiff := []byte("II*\x00\x08\x00\x00\x00" + // little-endian TIFF header, IFD at 8
		"\x01\x00" + // one entry
		"\x12\x01\x03\x00\x01\x00\x00\x00\x00\x00\x00\x00" + // 0x0112 Orientation, SHORT, count 1
		"\x00\x00\x00\x00") // no next IFD
	binary.LittleEndian.PutUint16(tiff[18:20], uint16(orientation)) //nolint:gosec // orientation is 1–8
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	b := []byte("\xFF\xD8\xFF\xE1\x00\x00")
	binary.BigEndian.PutUint16(b[4:6], uint16(len(app1)+2)) //nolint:gosec // segment is small
	b = append(b, app1...)
	return append(b, "\xFF\xD9"...)
}
