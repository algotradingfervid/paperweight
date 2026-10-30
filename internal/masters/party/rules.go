package party

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party/dbq"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The document fields that link a party in each role (MST-043).
var (
	vendorFields   = []string{"vendor_id", "supplier_id"}
	customerFields = []string{"customer_id"}
)

// checkRolesInUse refuses to untick a role the party is used in (MST-043).
// Documents are read through References, at any state; a project's customer
// is a master link, which References doesn't cover (CD-167).
func (s *Service) checkRolesInUse(ctx context.Context, tx *db.Tx, old Party, in Input) error {
	var es mstkit.Errors
	refuse := func(field, role, where string) {
		es.Add(field, old.Code+" is used as a "+role+" on "+where+". Disable the party instead.")
	}
	if old.IsVendor && !in.IsVendor {
		where, err := s.documentUsing(ctx, tx, old.ID, vendorFields)
		if err != nil {
			return err
		}
		if where != "" {
			refuse("is_vendor", "vendor", where)
		}
	}
	if old.IsCustomer && !in.IsCustomer {
		where, err := s.documentUsing(ctx, tx, old.ID, customerFields)
		if err != nil {
			return err
		}
		if where == "" {
			projects, err := dbq.New(tx.SQL()).ProjectsOfCustomer(ctx, sql.NullInt64{Int64: old.ID, Valid: true})
			if err != nil {
				return err
			}
			if len(projects) > 0 {
				where = "Project " + projects[0]
			}
		}
		if where != "" {
			refuse("is_customer", "customer", where)
		}
	}
	return es.Err()
}

// documentUsing is the display of the first document linking the party
// through one of fields, on its header or lines, "" when none does.
func (s *Service) documentUsing(ctx context.Context, tx *db.Tx, id int64, fields []string) (string, error) {
	// DOC's limit also cuts the merged list, so this reads every reference.
	refs, err := s.env.References(ctx, tx, Kind.Table, id, math.MaxInt32)
	if err != nil {
		return "", err
	}
	for _, r := range refs {
		if slices.Contains(fields, strings.TrimPrefix(r.Field, "lines.")) {
			return r.Display, nil
		}
	}
	return "", nil
}

// checkTerms refuses unknown or disabled default payment terms; 0 is none. A
// link the user can edit is re-checked on every save.
func (s *Service) checkTerms(ctx context.Context, tx *db.Tx, id int64) error {
	if id == 0 {
		return nil
	}
	var es mstkit.Errors
	t, err := paymentterms.New(s.env).Get(ctx, tx, id)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("default_payment_terms_id", MsgTermsNotFound)
	case err != nil:
		return err
	case t.Disabled:
		es.Add("default_payment_terms_id", "Payment terms "+t.Code+" is disabled. Pick another payment terms.")
	}
	return es.Err()
}

// saveWarnings warns of other parties with the name or the PAN a save gave
// the party, compared with old (MST-045).
func (s *Service) saveWarnings(ctx context.Context, tx *db.Tx, id int64, in Input, old Party) ([]string, error) {
	var out []string
	if in.Name != old.Name {
		named, err := s.nameWarnings(ctx, tx, id, in.Name)
		if err != nil {
			return nil, err
		}
		out = named
	}
	if in.PAN != old.PAN {
		same, err := s.SamePANWarnings(ctx, tx, id, in.PAN, "")
		if err != nil {
			return nil, err
		}
		out = append(out, same...)
	}
	return out, nil
}

// nameWarnings warns of other parties with the same name, in any case
// (MST-045).
func (s *Service) nameWarnings(ctx context.Context, tx *db.Tx, id int64, name string) ([]string, error) {
	rows, err := dbq.New(tx.SQL()).OtherPartiesNamed(ctx, dbq.OtherPartiesNamedParams{Name: name, ID: id})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		out = append(out, "A party named "+r.Name+" already exists ("+r.Code+").")
	}
	return out, nil
}

// SamePANWarnings warns of parties other than partyID with pan, suggesting an
// address in state on them instead when state isn't "" (MST-045). A blank pan
// warns of nothing.
func (s *Service) SamePANWarnings(ctx context.Context, tx *db.Tx, partyID int64, pan, state string) ([]string, error) {
	if pan == "" {
		return nil, nil
	}
	codes, err := dbq.New(tx.SQL()).OtherPartiesWithPAN(ctx, dbq.OtherPartiesWithPANParams{Pan: pan, ID: partyID})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, code := range codes {
		msg := "Party " + code + " has the same PAN " + pan + ". You may be creating a duplicate"
		if state != "" {
			msg += "; consider adding a " + state + " address to " + code
		}
		out = append(out, msg+".")
	}
	return out, nil
}
