package docscreen

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// searchShown is how many rows a group shows. The query keeps the normal page
// size: the kernel refuses a size of 5.
const searchShown = 5

// searchGroup is one DocType's results; SeeAll is nil when every row shows.
type searchGroup struct {
	Heading string
	List    components.DataListProps
	SeeAll  *components.Link
}

// searchView is everything the search page renders; Message replaces the
// groups when there are none.
type searchView struct {
	Opts    layout.PageOpts
	Bar     components.FilterBarProps
	Message string
	Groups  []searchGroup
}

// search serves GET /search?q= (design-system §6, CD-200 part 4). It lives
// outside /doc/, so WP3 mounts it with AUTH's Self guard, not CheckedInHandler
// (CD-87): any signed-in user may search, and the rights are checked per
// DocType in searchGroups.
func (d Deps) search(w http.ResponseWriter, r *http.Request) {
	q := searchText(r.URL.Query().Get("q"))
	v := searchView{
		Opts: layout.PageOpts{Title: "Search"},
		Bar:  components.FilterBarProps{Action: "/search", Search: q, SearchLabel: "Search"},
	}
	if q == "" {
		v.Message = "Type a name, code or number to search."
	} else {
		groups, err := d.searchGroups(r.Context(), d.principal(r), q)
		if err != nil {
			problem(w, r, err)
			return
		}
		v.Groups = groups
		if len(groups) == 0 {
			v.Message = `Nothing found for "` + q + `".`
		}
	}
	ui.Render(w, r, http.StatusOK, searchPage(v), searchMain(v))
}

// searchText is the trimmed query, cut to the header input's maxlength for a
// URL typed by hand.
func searchText(raw string) string {
	q := strings.TrimSpace(raw)
	if r := []rune(q); len(r) > layout.SearchMaxLen {
		return string(r[:layout.SearchMaxLen])
	}
	return q
}

// searchGroups runs q on every DocType with SearchFields that p can read,
// through the Lister and so its read scope: rows, counts and See all cover
// only what p may see. A refused read skips the DocType; any other failure is
// an error, never "nothing found".
func (d Deps) searchGroups(ctx context.Context, p auth.Principal, q string) ([]searchGroup, error) {
	var groups []searchGroup
	for _, dt := range doctype.All() {
		if len(dt.SearchFields) == 0 {
			continue
		}
		err := d.Auth.Can(ctx, p, dt.Name, "read", nil)
		if errors.Is(err, auth.ErrForbidden) {
			continue
		}
		if err != nil {
			return nil, err
		}
		res, err := d.Lister.List(ctx, p, dt, ListQuery{Search: q, Page: 1, Size: defaultPageSize})
		if err != nil {
			return nil, err
		}
		if res.Total == 0 {
			continue
		}
		cols := ListColumns(dt)
		rows, err := d.listRows(ctx, dt, cols, res.Rows[:min(searchShown, len(res.Rows))], d.recordURL)
		if err != nil {
			return nil, err
		}
		count := types.FormatCount(int64(res.Total))
		g := searchGroup{
			Heading: dt.PluralLabel + " (" + count + ")",
			List:    components.DataListProps{Label: dt.PluralLabel, Columns: cols, Rows: rows},
		}
		if res.Total > searchShown {
			g.SeeAll = &components.Link{Label: "See all " + count + " " + dt.PluralLabel, Href: d.seeAllURL(dt.Name, q)}
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (d Deps) recordURL(name string, id int64) string {
	if d.RecordURL != nil {
		return d.RecordURL(name, id)
	}
	return docURL(name, id)
}

func (d Deps) seeAllURL(name, q string) string {
	query := ListQuery{Search: q}
	if d.ListURL != nil {
		return d.ListURL(name) + "?" + query.Encode()
	}
	return listURL(name, query)
}
