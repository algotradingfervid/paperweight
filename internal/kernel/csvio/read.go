package csvio

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/types"
)

const (
	MaxFileBytes = 5 << 20
	MaxRows      = 5000
)

// Sheet is a parsed CSV file: the header row and the non-empty data rows.
type Sheet struct {
	Header []string
	Rows   []SheetRow
}

// SheetRow is one data row; Num is the spreadsheet row number (header = 1).
type SheetRow struct {
	Num   int
	Cells []string
}

var utf8BOM = []byte("\uFEFF")

// ReadSheet reads a UTF-8 CSV file, trims every cell, skips all-empty rows
// and numbers rows the way Excel shows them.
func ReadSheet(r io.Reader) (*Sheet, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, &FileError{Msg: "This file is larger than 5 MB. Split it into smaller files."}
	}
	if !utf8.Valid(data) {
		return nil, &FileError{Msg: "This file isn't saved as UTF-8. In Excel choose Save As → CSV UTF-8 (Comma delimited) and upload again."}
	}
	data = bytes.TrimPrefix(data, utf8BOM)

	s, total, err := readRows(data)
	if err != nil {
		return nil, err
	}
	if total > MaxRows {
		return nil, &FileError{Msg: "This file has " + types.FormatCount(int64(total)) + " rows. Import at most 5,000 rows at a time."}
	}
	if total == 0 {
		return nil, &FileError{Msg: "The file has no rows to import."}
	}
	return s, nil
}

// readRows parses data, keeping at most MaxRows rows but counting them all.
func readRows(data []byte) (*Sheet, int, error) {
	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = false
	s := &Sheet{}
	newlinesBefore := 0 // "\n" inside cells of earlier records
	total := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			// CD-132: quote errors get the quote-mark message; any other parse error gets the generic one.
			row := pe.StartLine - newlinesBefore
			if errors.Is(pe.Err, csv.ErrBareQuote) || errors.Is(pe.Err, csv.ErrQuote) {
				return nil, 0, &FileError{Msg: fmt.Sprintf("Row %d has a quote mark (\") in the wrong place. Save the file again as CSV and try once more.", row)}
			}
			return nil, 0, &FileError{Msg: fmt.Sprintf("Row %d can't be read. Save the file again as CSV and try once more.", row)}
		}
		if err != nil {
			return nil, 0, err
		}
		line, _ := cr.FieldPos(0)
		num := line - newlinesBefore
		empty := true
		for i, c := range rec {
			newlinesBefore += strings.Count(c, "\n")
			rec[i] = strings.TrimSpace(c)
			if rec[i] != "" {
				empty = false
			}
		}
		if empty {
			continue
		}
		if s.Header == nil {
			s.Header = rec
			continue
		}
		if len(rec) != len(s.Header) {
			return nil, 0, &FileError{Msg: fmt.Sprintf("Row %d has %d values but the header has %d. Check for an unquoted comma.", num, len(rec), len(s.Header))}
		}
		total++
		if len(s.Rows) < MaxRows {
			s.Rows = append(s.Rows, SheetRow{Num: num, Cells: rec})
		}
	}
	return s, total, nil
}
