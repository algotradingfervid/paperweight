package doc

// Gone is the KindNotFound error of a missing document, exported so docfake's
// Load and Info return exactly what the engine's do (CD-125, CD-186).
func Gone() *Error { return errGone() }
