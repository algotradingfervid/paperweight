// Package errs holds errors shared across the platform.
package errs

import "errors"

// ErrNotImplemented is returned by stubs whose owning work package has not built them yet.
var ErrNotImplemented = errors.New("not implemented yet")
