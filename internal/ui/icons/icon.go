// Package icons renders the vendored Lucide icons listed in icons.txt.
package icons

//go:generate go run ./gen

// Names returns the icon names in icons.txt order.
func Names() []string {
	return append([]string(nil), names...)
}
