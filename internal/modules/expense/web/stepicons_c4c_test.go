package expenseweb_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4c: each step's Check card has its own tile.
func TestCheckTiles_Claim(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	claim := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	uitest.WantCheckTiles(t, e.page(t, s2Deepak, "/doc/"+expense.ClaimDocType+"/"+strconv.FormatInt(claim.ID, 10)+"/edit/check"),
		uitest.CheckTile{Title: "Claim details", Icon: "receipt-indian-rupee"},
		uitest.CheckTile{Title: "Expenses", Icon: "list-checks"})
}

func TestCheckTiles_Advance(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	adv := e.DraftAdvance(t, e.P(t, s2Deepak), nil)
	uitest.WantCheckTiles(t, e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/"+strconv.FormatInt(adv.ID, 10)+"/edit/check"),
		uitest.CheckTile{Title: "Details", Icon: "hand-coins"},
		uitest.CheckTile{Title: "Items", Icon: "list-checks"})
}
