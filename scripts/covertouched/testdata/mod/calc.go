// Package calc is a coverage fixture for covertouched.
package calc

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrNegative refuses a negative number.
var ErrNegative = errors.New("negative")

// Parse reads a non-negative number.
func Parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, ErrNegative
	}
	return n, nil
}

// Wrap parses s and names it in the error.
func Wrap(s string) (int, error) {
	n, err := Parse(s)
	if err != nil {
		return 0, fmt.Errorf("wrap %q: %w", s, err)
	}
	return n, nil
}
