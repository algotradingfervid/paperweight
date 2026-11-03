package namingweb

import "testing"

// UI v2 R10 (shell M12): a series in words, for every token a template can
// hold, and a number with nothing before it.
func TestSeriesWords(t *testing.T) {
	for template, want := range map[string]string{
		"PO/{FY}/{#####}":      "PO / financial year / 5-digit number",
		"{####}":               "4-digit number",
		"INV/{YYYY}/{MM}/{##}": "INV / year / month / 2-digit number",
		"GR-{YY}-{###}":        "GR-year-3-digit number",
	} {
		if got := seriesWords(template); got != want {
			t.Errorf("seriesWords(%q) = %q; want %q", template, got, want)
		}
	}
}
