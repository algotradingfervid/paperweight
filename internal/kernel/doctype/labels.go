package doctype

import "strconv"

var stateLabels = map[State]string{
	StateDraft:     "Draft",
	StatePending:   "Waiting for approval",
	StateApproved:  "Approved",
	StateClosed:    "Closed",
	StateRejected:  "Rejected",
	StateCancelled: "Cancelled",
}

// StateLabel is the label users see for s.
func StateLabel(s State) string { return stateLabels[s] }

// Actions returns every Action in §5 order.
func Actions() []Action {
	return []Action{ActionSave, ActionSubmit, ActionWithdraw, ActionApprove, ActionReject,
		ActionRevise, ActionClose, ActionCancel, ActionAmend, ActionDelete}
}

// Path is the form path of the field: "supplier_id" on the header (Line 0),
// "lines[2].qty" on line 2.
func (e FieldError) Path() string {
	if e.Line == 0 {
		return e.Field
	}
	return "lines[" + strconv.Itoa(e.Line) + "]." + e.Field
}

// IsDocument reports whether dt runs through the document engine.
func (dt *DocType) IsDocument() bool {
	return dt.Hooks != nil && dt.Master == nil && dt.LinesTable != ""
}

// IsMaster reports whether dt is a master (CD-21).
func (dt *DocType) IsMaster() bool { return dt.Master != nil }

// Field returns the field called name.
func (dt *DocType) Field(name string) (Field, bool) {
	for _, f := range dt.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// HeaderFields returns the fields that are not OnLine, in declared order.
func (dt *DocType) HeaderFields() []Field { return dt.fields(false) }

// LineFields returns the OnLine fields, in declared order.
func (dt *DocType) LineFields() []Field { return dt.fields(true) }

func (dt *DocType) fields(onLine bool) []Field {
	var out []Field
	for _, f := range dt.Fields {
		if f.OnLine == onLine {
			out = append(out, f)
		}
	}
	return out
}
