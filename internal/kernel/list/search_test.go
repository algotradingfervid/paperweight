package list_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
)

func search(s string) list.Query { return list.Query{Search: s} }

func TestList_SearchFields(t *testing.T) { // NUM-119
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	got, _ := listtest.Run(t, d, listtest.Party(), search("  local "), noScope)
	sameIDs(t, `search "  local "`, got, ids["LOCAL"])
	got, _ = listtest.Run(t, d, listtest.Party(), search("cables"), noScope)
	sameIDs(t, `search "cables"`, got, ids["COROM"])

	approved := listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00042", Status: "approved", PartyName: "Shree Steel Traders"})
	draft := listtest.InsertPO(t, d, listtest.PORow{Status: "draft", PartyName: "Bharat Tools"})
	for s, want := range map[string][]int64{
		"00042":  {approved},
		"shree":  {approved},
		"bharat": {draft},
		"":       {approved, draft},
	} {
		got, _ := listtest.Run(t, d, listtest.PO(), search(s), noScope)
		sameIDs(t, "PO search "+s, got, want...)
	}
}

func TestList_SearchLiteralChars(t *testing.T) { // NUM-120
	d := listtest.Setup(t)
	listtest.SeedFixtures(t, d)
	pct := listtest.InsertItem(t, d, listtest.ItemRow{Code: "BOLT-10P", Name: "Bolt 10%"})
	listtest.InsertItem(t, d, listtest.ItemRow{Code: "BOLT-100", Name: "Bolt 100"})
	slash := listtest.InsertItem(t, d, listtest.ItemRow{Code: "AB-PLATE", Name: `A\B Plate`})

	got, _ := listtest.Run(t, d, listtest.Item(), search("10%"), noScope)
	sameIDs(t, `search "10%"`, got, pct)
	got, _ = listtest.Run(t, d, listtest.Item(), search(`\`), noScope)
	sameIDs(t, `search "\"`, got, slash)
	got, _ = listtest.Run(t, d, listtest.Item(), search("_"), noScope)
	sameIDs(t, `search "_"`, got)
}
