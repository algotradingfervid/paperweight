// Package docscreen is the generic DocType screens under /doc/{doctype}: the
// list, the document page, the wizard form, the line pages and the reason
// and confirm pages. Data with a §5 contract is read through it; the rest
// sits behind the consumer interfaces Lister and Extras, which WP3 adapts
// (CD-78).
package docscreen

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Reader runs fn in a read transaction; *db.DB satisfies it.
type Reader interface {
	Read(ctx context.Context, fn func(tx *db.Tx) error) error
}

// Filter is one list filter; Values for the same Field and Op are grouped.
type Filter struct {
	Field, Op string
	Values    []string
}

// ListQuery is a list's state as carried in its URL (NUM-125).
type ListQuery struct {
	Search, Sort string
	Filters      []Filter
	Page, Size   int
}

// ParseListQuery reads q, sort (<field>.asc|desc), page, size and repeated
// f=<field>:<op>:<value> parameters.
func ParseListQuery(v url.Values) ListQuery {
	q := ListQuery{Search: v.Get("q"), Sort: v.Get("sort")}
	q.Page, _ = strconv.Atoi(v.Get("page"))
	q.Size, _ = strconv.Atoi(v.Get("size"))
	for _, raw := range v["f"] {
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) != 3 {
			continue
		}
		i := slices.IndexFunc(q.Filters, func(f Filter) bool { return f.Field == parts[0] && f.Op == parts[1] })
		if i < 0 {
			q.Filters = append(q.Filters, Filter{Field: parts[0], Op: parts[1]})
			i = len(q.Filters) - 1
		}
		q.Filters[i].Values = append(q.Filters[i].Values, parts[2])
	}
	return q
}

// Encode is the query string of q with sorted keys, so URLs are stable.
func (q ListQuery) Encode() string {
	v := url.Values{}
	if q.Search != "" {
		v.Set("q", q.Search)
	}
	if q.Sort != "" {
		v.Set("sort", q.Sort)
	}
	if q.Page > 0 {
		v.Set("page", strconv.Itoa(q.Page))
	}
	if q.Size > 0 {
		v.Set("size", strconv.Itoa(q.Size))
	}
	for _, f := range q.Filters {
		for _, val := range f.Values {
			v.Add("f", f.Field+":"+f.Op+":"+val)
		}
	}
	return v.Encode()
}

// ListRow is one list result; Values are plain, keyed by Field.Name.
type ListRow struct {
	Info   doctype.Info
	Values map[string]string
}

// Lister runs a list query (WP3: list.Build + auth.Scope + db.Read).
type Lister interface {
	List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q ListQuery) (types.Result[ListRow], error)
}

// ChangeRow is one changed field in a history entry.
type ChangeRow struct{ Field, Before, After string }

// HistoryRow is one approval or audit history entry, already formatted.
type HistoryRow struct {
	At, By, Action string
	Changes        []ChangeRow
}

// ApprovalView is what the document page shows about approval.
type ApprovalView struct {
	Needed             bool
	Preview, WaitingOn string
	History            []HistoryRow
}

// Origin holds DOC's state_reason and amended_from_id, which doc.Record
// does not carry.
type Origin struct {
	StateReason, AmendedFrom string
	AmendedFromID            int64
}

// Extras reads what the document page needs beyond doc.Record (CD-78).
type Extras interface {
	Approval(ctx context.Context, p auth.Principal, doctype string, id int64) (ApprovalView, error)
	Origin(ctx context.Context, p auth.Principal, doctype string, id int64) (Origin, error)
	Audit(ctx context.Context, p auth.Principal, doctype string, id int64) ([]HistoryRow, error)
	Attachments(ctx context.Context, p auth.Principal, rec auth.RecordRef) ([]components.Attachment, error)
}

// Deps is everything the docscreen routes read from.
type Deps struct {
	DB        Reader
	Engine    doc.Engine
	Auth      auth.Service
	Approval  approval.Service
	Lister    Lister
	Extras    Extras
	Principal func(*http.Request) (auth.Principal, bool) // nil → auth.PrincipalFrom(r.Context())
	Today     func() (today, yesterday string)
	InboxURL  string // "/approvals"
	PrintURL  func(doctype string, id int64) string
	Parse     map[string]components.ParseFunc // nil → components.NUMParsers(); WP3 adds the code kinds
}

func (d Deps) principal(r *http.Request) auth.Principal {
	if d.Principal != nil {
		p, _ := d.Principal(r)
		return p
	}
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}
