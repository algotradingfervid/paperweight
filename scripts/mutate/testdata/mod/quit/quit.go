// Package quit has mutants that exit the test binary without a test failure.
package quit

import "os"

// Quit exits when code is positive.
func Quit(code int) {
	if code > 0 {
		os.Exit(2)
	}
}
