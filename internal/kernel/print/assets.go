package print

import _ "embed"

// CSS is the print stylesheet served at /print/assets/print.css.
//
//go:embed print.css
var CSS string
