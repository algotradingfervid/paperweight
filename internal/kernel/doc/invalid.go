package doc

// Invalid is the exported twin of invalid: a module's BeforeSubmit returns it
// so the check page highlights the fields (KindInvalid, the fix-fields text,
// 422; CD-264).
func Invalid(fields []FieldError) *Error {
	return invalid(fields)
}
