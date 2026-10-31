package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-PO P2 Do (PO spec §4; PO-010, PO-011): checkSiteProject, which P5
// reuses for requests, returns the project defaulted from the site when it is
// blank, refuses a site on another project with §4's text, and leaves a site
// with no project (or no site) alone.
func TestCheckSiteProject(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	id := func(table, code string) int64 { t.Helper(); return p2Int(t, masterID(t, env.DB, table, code)) }
	metro, tech, wh := id("site", "METRO-SITE"), id("site", "TECH-SITE"), id("site", "PUNE-WH")
	prjMetro, prjTech := id("project", "PRJ-METRO"), id("project", "PRJ-TECHPARK")

	for _, c := range []struct {
		what          string
		site, project int64
		defaulted     int64
		refused       bool
	}{
		{"site and its own project", metro, prjMetro, prjMetro, false},
		{"blank project takes the site's", metro, 0, prjMetro, false},
		{"another project", tech, prjMetro, 0, true},
		{"site with no project keeps any", wh, prjTech, prjTech, false},
		{"site with no project, blank project", wh, 0, 0, false},
		{"no site", 0, prjTech, prjTech, false},
	} {
		var got int64
		var fe *doc.FieldError
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			got, fe, err = purchase.CheckSiteProject(ctx, tx, c.site, c.project)
			return err
		}); err != nil {
			t.Errorf("%s: err = %v", c.what, err)
			continue
		}
		if c.refused {
			if fe == nil || fe.Line != 0 || (fe.Field != "site_id" && fe.Field != "project_id") ||
				fe.Message != "Site TECH-SITE is on project PRJ-TECHPARK. Pick that project or another site." {
				t.Errorf("%s: field error %+v, want §4's text on site_id or project_id", c.what, fe)
			}
			continue
		}
		if fe != nil || got != c.defaulted {
			t.Errorf("%s: = %d, %+v; want %d, nil", c.what, got, fe, c.defaulted)
		}
	}
}
