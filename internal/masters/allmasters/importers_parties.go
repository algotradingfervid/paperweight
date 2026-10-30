package allmasters

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// partyImporters is the Parties group's RowFuncs (Task 20).
func partyImporters() map[string]rowFunc {
	return map[string]rowFunc{
		party.DocType:          (*Services).importParty,
		address.DocType:        (*Services).importAddress,
		address.ContactDocType: (*Services).importContact,
	}
}

func (*Services) importParty(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Party import not implemented")
}

func (*Services) importAddress(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Address import not implemented")
}

func (*Services) importContact(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Contact import not implemented")
}
