package doctype

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
)

var (
	identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`) // tables and columns
	stepRe  = regexp.MustCompile(`^[a-z0-9-]*$`)       // wizard step slugs
)

// Columns every header and line table has by the document table convention.
var (
	headerConvColumns = []string{"id", "number", "state", "state_reason", "amended_from_id", "amendment_root_id",
		"amendment_no", "version", "created_at", "created_by", "updated_at", "updated_by"}
	lineConvColumns = []string{"id", "parent_id", "line_no"}
)

type registry struct {
	mu     sync.RWMutex
	byName map[string]*DocType
}

func newRegistry() *registry { return &registry{byName: map[string]*DocType{}} }

var std = newRegistry()

// Register adds dt at init. It panics on an empty Name, a duplicate, a bad
// table or column identifier, a bad field or a master that has Hooks or is
// submittable or approvable. References to other DocTypes are checked by Check.
func Register(dt DocType) { std.Register(dt) }

// Lookup returns the DocType registered under name.
func Lookup(name string) (*DocType, bool) { return std.Lookup(name) }

// All returns every registered DocType, sorted by Name.
func All() []*DocType { return std.All() }

// CheckRegistry resolves every cross-DocType reference: Field.Link, RollUps,
// Links and the rights fields. doc.NewEngine calls it. (The plan's name Check
// is taken by the FieldType constant.)
func CheckRegistry() error { return std.Check() }

func (r *registry) Register(dt DocType) {
	if dt.Name == "" {
		panic("doctype: DocType has no Name")
	}
	dt.Fields = slices.Clone(dt.Fields)
	for i := range dt.Fields {
		if dt.Fields[i].Column == "" {
			dt.Fields[i].Column = dt.Fields[i].Name
		}
	}
	if err := validate(&dt); err != nil {
		panic(fmt.Sprintf("doctype: DocType %q: %v", dt.Name, err))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.byName[dt.Name]; dup {
		panic(fmt.Sprintf("doctype: DocType %q registered twice", dt.Name))
	}
	r.byName[dt.Name] = &dt
}

func (r *registry) Lookup(name string) (*DocType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dt, ok := r.byName[name]
	return dt, ok
}

func (r *registry) All() []*DocType {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]*DocType, 0, len(r.byName))
	for _, dt := range r.byName {
		all = append(all, dt)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all
}

// validate checks the rules that need only dt itself.
func validate(dt *DocType) error {
	if dt.Table != "" && !identRe.MatchString(dt.Table) {
		return fmt.Errorf("Table %q is not a valid identifier", dt.Table)
	}
	if dt.LinesTable != "" && !identRe.MatchString(dt.LinesTable) {
		return fmt.Errorf("LinesTable %q is not a valid identifier", dt.LinesTable)
	}
	if dt.Master != nil && (dt.Hooks != nil || dt.Submittable || dt.Approvable) {
		return errors.New("a master can't have Hooks or be Submittable or Approvable")
	}
	seen := map[string]bool{}
	columns := map[bool]map[string]string{false: {}, true: {}} // OnLine -> column -> field name
	for _, f := range dt.Fields {
		if seen[f.Name] {
			return fmt.Errorf("field %q declared twice", f.Name)
		}
		seen[f.Name] = true
		if err := validateField(f); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
		conv := headerConvColumns
		if f.OnLine {
			conv = lineConvColumns
		}
		if slices.Contains(conv, f.Column) {
			return fmt.Errorf("field %q: Column %q is a convention column", f.Name, f.Column)
		}
		if other, dup := columns[f.OnLine][f.Column]; dup {
			return fmt.Errorf("fields %q and %q share Column %q", other, f.Name, f.Column)
		}
		columns[f.OnLine][f.Column] = f.Name
	}
	return nil
}

func validateField(f Field) error {
	switch {
	case !identRe.MatchString(f.Column):
		return fmt.Errorf("Column %q is not a valid identifier", f.Column)
	case f.Type == Link && f.Link == "":
		return errors.New("a Link field must name its target DocType")
	case f.Type == Select && len(f.Options) == 0:
		return errors.New("a Select field needs Options")
	case f.Type != Select && len(f.Options) > 0:
		return errors.New("only a Select field can have Options")
	case f.ListPriority < 0:
		return errors.New("ListPriority can't be negative")
	case f.MaxLen < 0:
		return errors.New("MaxLen can't be negative")
	case !stepRe.MatchString(f.Step):
		return fmt.Errorf("Step %q is not a slug", f.Step)
	case f.EditableAfterSubmit && !f.RollUp && !f.System:
		return errors.New("EditableAfterSubmit needs RollUp or System")
	case f.ReadOnly && (f.Required || f.Default != ""):
		return errors.New("a ReadOnly field can't be Required or have a Default")
	}
	return nil
}

func (r *registry) Check() error {
	var errs []error
	for _, dt := range r.All() {
		bad := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("doctype: DocType %q: "+format, append([]any{dt.Name}, args...)...))
		}
		for _, f := range dt.Fields {
			if f.Type == Link {
				if _, ok := r.Lookup(f.Link); !ok {
					bad("field %q links to unknown DocType %q", f.Name, f.Link)
				}
			}
		}
		for _, ru := range dt.RollUps {
			id := ru.TargetDocType + "." + ru.TargetField
			for _, name := range []string{ru.SourceField, ru.JoinField} {
				if f, ok := dt.Field(name); !ok {
					bad("roll-up %s names unknown field %q", id, name)
				} else if !f.OnLine {
					bad("roll-up %s: field %q is not a line field", id, name)
				}
			}
			target, ok := r.Lookup(ru.TargetDocType)
			if !ok {
				bad("roll-up targets unknown DocType %q", ru.TargetDocType)
			} else if f, ok := target.Field(ru.TargetField); !ok {
				bad("roll-up targets unknown field %q on %q", ru.TargetField, ru.TargetDocType)
			} else if !f.OnLine {
				bad("roll-up %s: target field %q is not a line field", id, ru.TargetField)
			}
		}
		for _, l := range dt.Links {
			if f, ok := dt.Field(l.Field); !ok {
				bad("link names unknown field %q", l.Field)
			} else if f.OnLine != l.OnLine {
				bad("link %q: OnLine is %v but the field's OnLine is %v", l.Field, l.OnLine, f.OnLine)
			}
			if _, ok := r.Lookup(l.Target); !ok {
				bad("link %q targets unknown DocType %q", l.Field, l.Target)
			}
		}
		rights := slices.Concat(dt.ProjectFields, dt.SiteFields)
		for _, c := range []string{dt.OwnerField, dt.EmployeeField} {
			if c != "" {
				rights = append(rights, c)
			}
		}
		for _, c := range rights {
			if !dt.hasColumn(c) {
				bad("rights field %q names no column", c)
			}
		}
	}
	return errors.Join(errs...)
}

// hasColumn reports whether c (a header column, or "lines.col" for a line
// column) exists on dt, counting the document table convention's columns.
func (dt *DocType) hasColumn(c string) bool {
	if col, ok := strings.CutPrefix(c, "lines."); ok {
		if slices.Contains(lineConvColumns, col) {
			return true
		}
		return slices.ContainsFunc(dt.LineFields(), func(f Field) bool { return f.Column == col })
	}
	if slices.Contains(headerConvColumns, c) {
		return true
	}
	return slices.ContainsFunc(dt.HeaderFields(), func(f Field) bool { return f.Column == c })
}
