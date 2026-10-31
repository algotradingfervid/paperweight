// Package slow has a mutant that loops forever.
package slow

// Wait returns once done is true.
func Wait(done bool) {
	for !done {
	}
}
