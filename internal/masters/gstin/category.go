package gstin

import (
	"errors"
	"fmt"
)

// Category is a party's GST category (D-MST-09).
type Category string

// The v1 GST categories, in display order.
const (
	Regular      Category = "Registered Regular"
	Composition  Category = "Registered Composition"
	Unregistered Category = "Unregistered"
	SEZ          Category = "SEZ"
	Overseas     Category = "Overseas"
)

// Categories returns the v1 categories in display order.
func Categories() []Category {
	return []Category{Regular, Composition, Unregistered, SEZ, Overseas}
}

// ValidateCategory checks that a normalised GSTIN fits category c
// (utils/__init__.py:413, validate_gst_category; GSTIN_FORMATS,
// constants/__init__.py:1461). Registered Regular also accepts the TCS and TDS
// formats, and a UIN GSTIN is refused for every category (M6, CD-39).
func ValidateCategory(c Category, gstin string) error {
	if gstin == "" {
		if c != Unregistered && c != Overseas {
			return errors.New("Enter a GSTIN, or set GST category to Unregistered or Overseas.")
		}
		return nil
	}
	if unbodyPattern.MatchString(gstin) {
		return errUnsupported
	}
	if c == Unregistered {
		return errors.New("GST category can't be Unregistered when a GSTIN is entered.")
	}
	var ok bool
	switch c {
	case Regular:
		ok = registered(gstin) || tcsPattern.MatchString(gstin) || tdsPattern.MatchString(gstin)
	case Composition, SEZ:
		ok = registered(gstin)
	case Overseas:
		ok = overseas(gstin)
	}
	if !ok {
		return fmt.Errorf("GSTIN %s doesn't match GST category %s. Check the GSTIN and the category.", gstin, c)
	}
	return nil
}

// GuessCategory guesses the category from a normalised GSTIN and the country,
// keeping a user's registered category that the GSTIN fits
// (utils/__init__.py:509, guess_gst_category). TDS and TCS GSTINs give
// Registered Regular; a UIN GSTIN is refused (M6, CD-39).
func GuessCategory(gstin, country string, current Category) (Category, error) {
	switch {
	case gstin == "":
		if country != "" && country != "India" || country == "" && current == Overseas {
			return Overseas, nil
		}
		return Unregistered, nil
	case tdsPattern.MatchString(gstin), tcsPattern.MatchString(gstin):
		return Regular, nil
	case registered(gstin):
		if current == Regular || current == Composition || current == SEZ {
			return current, nil
		}
		return Regular, nil
	case unbodyPattern.MatchString(gstin):
		return "", errUnsupported
	case overseas(gstin):
		return Overseas, nil
	}
	return Regular, nil // utils/__init__.py:546, "eg: e-Commerce Operator (TCS)"
}

var errUnsupported = errors.New("This GSTIN category isn't supported yet.")
