// Package gstin validates GSTINs (spec §5). MST owns it.
package gstin

import "github.com/vardhan/internaltools/internal/platform/errs"

func ValidateGSTIN(s string) error { return errs.ErrNotImplemented }

func StateCode(gstin string) string { return "" }

func PANFromGSTIN(gstin string) string { return "" }
