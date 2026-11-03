package partyweb

import (
	"strings"
	"testing"
)

// UI v2 R10: an address or contact form's cards are checked when the routes
// are built, so a field can't fall off the form or sit in two cards.
func TestRowKind_CardsChecked(t *testing.T) {
	s := records{}
	for _, k := range []rowKind{s.addressKind(), s.contactKind()} {
		k.checkCards() // the real cards fit
		for name, edit := range map[string]func(k *rowKind){
			"left out": func(k *rowKind) { k.cards = k.cards[1:] },
			"unknown":  func(k *rowKind) { k.cards[0].Fields = append(append([]string{}, k.cards[0].Fields...), "nonsense") },
		} {
			t.Run(k.kind.DocType+" "+name, func(t *testing.T) {
				bad := k
				bad.cards = append(bad.cards[:0:0], k.cards...)
				edit(&bad)
				defer func() {
					got, _ := recover().(string)
					if !strings.HasPrefix(got, "mstweb: "+k.kind.DocType+": ") {
						t.Errorf("panic = %q", got)
					}
				}()
				bad.checkCards()
			})
		}
	}
}
