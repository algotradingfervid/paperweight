// Package icons renders the vendored Lucide icons listed in icons.txt.
package icons

//go:generate go run ./gen

// Every icon renders as <svg class="icon">. icons_gen.go is kept out of the
// Tailwind scan (its icon names match daisyUI classes), so the class is
// named here.

// Names returns the icon names in icons.txt order.
func Names() []string {
	return append([]string(nil), names...)
}
