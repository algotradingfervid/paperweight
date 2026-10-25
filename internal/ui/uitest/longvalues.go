package uitest

import "strings"

// Worst-case display values for the gallery and the full-route sweep (U6).
const (
	// LongName100 is a 100-rune party or item name made of words.
	LongName100 = "Shree Venkateshwara Agro Industries and Cold Storage Private Limited Unit Three Hosur Road Bengaluru"
	// LongCode60 is 60 characters with no break opportunity.
	LongCode60 = "27AAPFU0939F1ZV27AAPFU0939F1ZV27AAPFU0939F1ZV27AAPFU0939F1ZV"
	// MaxMoney is the largest amount the forms accept.
	MaxMoney = "₹99,99,99,999.99"
	// LongDevanagari is 40 runes of Devanagari.
	LongDevanagari = "श्री वेंकटेश्वर एग्रो इंडस्ट्रीज लिमिटेड"
)

// LongEmail is a 253-character address.
var LongEmail = strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".in"

// LongError is a 400-rune error message that contains LongCode60.
var LongError = "The GSTIN: " + LongCode60 + " does not match the state of the billing address. " +
	strings.Repeat("Check the party master, correct the address or the GSTIN, and save again. ", 4)[:278] + "."

// LineSample is one document line as display strings.
type LineSample struct {
	Item, Qty, Rate, Amount string
}

// Lines returns n lines of worst-case values.
func Lines(n int) []LineSample {
	out := make([]LineSample, n)
	for i := range out {
		out[i] = LineSample{Item: LongName100, Qty: "1,00,000 kg", Rate: "₹9,99,999.9999", Amount: MaxMoney}
	}
	return out
}
