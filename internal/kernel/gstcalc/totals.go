package gstcalc

func totals(lines []Line, rounded bool) DocTotals {
	d := DocTotals{Lines: make([]LineTax, len(lines))}
	for i, l := range lines {
		c, s, g := SplitGST(l.Amount, l.GSTRate, l.InterState)
		d.Lines[i] = LineTax{Taxable: l.Amount, CGST: c, SGST: s, IGST: g}
		d.Net += l.Amount
		d.CGST += c
		d.SGST += s
		d.IGST += g
	}
	d.Grand = d.Net + d.CGST + d.SGST + d.IGST
	d.Rounded = d.Grand
	if rounded {
		d.Rounded = d.Grand.RoundRupee()
		d.Adjustment = d.Rounded - d.Grand
	}
	d.Payable = d.Rounded
	return d
}
