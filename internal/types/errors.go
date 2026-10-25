package types

import "errors"

// InputError is a user-facing message for a value that can't be accepted.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

// ErrEmpty means a required value was blank; callers turn it into
// DOC's "<Label> is required.".
var ErrEmpty = errors.New("value is required")
