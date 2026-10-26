//go:build !e2e

package docscreen

import (
	"context"
	"errors"
)

// browserSubmit needs a browser, which only the e2e build links
// (setup_e2e.go); the screens' Setup runs only there.
func browserSubmit(context.Context, string, string) error {
	return errors.New("docscreen: screen setup runs only in the e2e build")
}
