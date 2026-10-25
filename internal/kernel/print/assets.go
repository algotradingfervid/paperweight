package print

import _ "embed"

// CSS is the print stylesheet served at /print/assets/print.css.
//
//go:embed print.css
var CSS string

// JS is the print toolbar script served at /print/assets/print.js.
//
//go:embed print.js
var JS string
