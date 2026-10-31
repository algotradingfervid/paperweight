// Package mod is the mutate fixture.
package mod

import (
	"errors"
	"strconv"
)

// Limit is the cap.
const Limit = 100 // mutate:ok only documents the cap

// MaxScore is the highest score; any change to it fails to build.
const MaxScore = uint8(255)

// Adult reports whether age is 18 or more.
func Adult(age int) bool {
	return age >= 18
}

// Check refuses a negative amount.
func Check(n int) error {
	if n < 0 {
		msg := "negative"
		return errors.New(msg)
	}
	return nil
}

var known = map[string]bool{"a": true}

// Known reports whether name is known.
func Known(name string) bool {
	if _, ok := known[name]; ok {
		return true
	}
	return false
}

// Parse parses s.
func Parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	return n, nil
}
