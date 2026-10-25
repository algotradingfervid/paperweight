// Package doctype holds the DocType registry and every DocType type (spec §5,
// CD-02). DOC owns it.
package doctype

import (
	"context"
	"sort"
	"sync"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

type State string // draft pending_approval approved closed rejected cancelled ("" for masters)

const (
	StateDraft     State = "draft"
	StatePending   State = "pending_approval"
	StateApproved  State = "approved"
	StateClosed    State = "closed"
	StateRejected  State = "rejected"
	StateCancelled State = "cancelled"
)

type Action string // save submit withdraw approve reject revise close cancel amend delete

const (
	ActionSave     Action = "save"
	ActionSubmit   Action = "submit"
	ActionWithdraw Action = "withdraw"
	ActionApprove  Action = "approve"
	ActionReject   Action = "reject"
	ActionRevise   Action = "revise"
	ActionClose    Action = "close"
	ActionCancel   Action = "cancel"
	ActionAmend    Action = "amend"
	ActionDelete   Action = "delete"
)

type FieldError struct {
	Field   string
	Line    int
	Message string
} // Line 0 = header

type FieldType int // Text Int Check Select Link Money Qty Rate Percent Factor Date Timestamp

const (
	Text FieldType = iota
	Int
	Check
	Select
	Link
	Money
	Qty
	Rate
	Percent
	Factor
	Date
	Timestamp
)

type Field struct {
	Name, Label, Column                                            string
	Type                                                           FieldType
	Link                                                           string // target DocType for Link
	Required, Filterable, Sortable, Searchable, Importable, OnLine bool
	ListPriority, MaxLen                                           int
	Options                                                        []string
	Hint, Step                                                     string // CD-42
	ReadOnly                                                       bool   // CD-79: computed fields; client values rejected
}

type SortKey struct {
	Field string
	Desc  bool
}

type LinkSpec struct {
	Field, Target string
	OnLine        bool
}

type RollUp struct{ TargetDocType, TargetField, SourceField, JoinField string }

type Hooks interface {
	Validate(ctx context.Context, tx *db.Tx, id int64) []FieldError
	BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error
	ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error
	ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error
	Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error)
	Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error)
}

type MasterInfo struct { // CD-21
	Code, Name       string
	Disabled         bool
	Blocked, Warning string
}

type MasterLookup interface {
	Lookup(ctx context.Context, tx *db.Tx, id int64) (MasterInfo, bool, error)
}

type DocType struct {
	Name, Label, PluralLabel, Table, LinesTable                     string // CD-35
	Series                                                          string
	Submittable, Approvable, Printable, Importable, HasRoundedTotal bool
	Fields                                                          []Field
	SearchFields                                                    []string
	DefaultSort                                                     []SortKey
	Links                                                           []LinkSpec
	RollUps                                                         []RollUp
	OwnerField, EmployeeField                                       string
	ProjectFields, SiteFields                                       []string                     // CD-09 (line columns as "lines.col")
	DefaultRights                                                   map[string]map[string]string // role → right → scope ("own" | "team" | "all"), CD-09/10/11
	Hooks                                                           Hooks                        // nil for masters
	Master                                                          MasterLookup                 // non-nil only for masters (CD-21)
}

// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
var registry = struct {
	mu     sync.Mutex
	byName map[string]*DocType
}{byName: map[string]*DocType{}}

// Register adds dt at init; it panics on an empty Name or a duplicate. DOC adds
// full spec validation later.
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
func Register(dt DocType) {
	if dt.Name == "" {
		panic("doctype.Register: DocType has no Name")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.byName[dt.Name]; dup {
		panic("doctype.Register: DocType " + dt.Name + " is already registered")
	}
	registry.byName[dt.Name] = &dt
}

// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
func Lookup(name string) (*DocType, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	dt, ok := registry.byName[name]
	return dt, ok
}

// All returns every registered DocType, sorted by Name.
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
func All() []*DocType {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	all := make([]*DocType, 0, len(registry.byName))
	for _, dt := range registry.byName {
		all = append(all, dt)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

type Info struct { // CD-05
	DocType, DocLabel   string
	ID                  int64
	State               State
	Number              string
	Display             string // "PO/26-27/00001" or "Draft 42"
	Party               string // CD-55
	CreatedBy           types.UserID
	EmployeeID          int64
	ProjectIDs, SiteIDs []int64
}

// statusError is a sentinel error that carries its HTTP status (CD-44).
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
type statusError struct {
	status int
	msg    string
}

func (e *statusError) Error() string             { return e.msg }
func (e *statusError) Status() int               { return e.status }
func (e *statusError) FieldErrors() []FieldError { return nil }

// ErrNotFound is the one not-found sentinel; auth.ErrNotFound is the same value (CD-54).
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
var ErrNotFound error = &statusError{status: 404, msg: "This record doesn't exist or you don't have access to it."}

type HTTPError interface { // CD-44: 403 404 409 422 503
	error
	Status() int
	FieldErrors() []FieldError
}

type InfoSource interface {
	Info(ctx context.Context, tx *db.Tx, doctype string, id int64) (Info, error) // ErrNotFound if missing
}
