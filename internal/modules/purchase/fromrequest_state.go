package purchase

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// FromRequestState is what the Create PO page shows (UX §5.18): the request
// with its requester, needed-by date and rows, each row's Pending as
// PendingToOrder gives it; the version the form posts; the site's code; and
// the vendor the page was posted with, by code and name, when there is one
// and p may read it (CD-233), else zero.
type FromRequestState struct {
	Request                OpenRequest
	Version                int
	SiteCode               string
	VendorCode, VendorName string
}

// FromRequestState reads the Create PO page's state for p and vendorID, with
// CreateFromRequest's load and right checks in its order: a missing or hidden
// request is AUTH's 404 (CD-245), no create on the PO AUTH's 403.
func (s *Service) FromRequestState(ctx context.Context, p auth.Principal, requestID, vendorID int64) (FromRequestState, error) {
	var st FromRequestState
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.fromRequestRights(ctx, tx, p, requestID)
		if err != nil {
			return err
		}
		req, err := doc.LoadDoc(ctx, tx, RequestDocType, requestID)
		if err != nil {
			return err
		}
		st.Version = req.Version
		siteID, _ := req.Header["site_id"].(int64)
		site, err := sites.Get(ctx, tx, siteID)
		if err != nil {
			return err
		}
		st.SiteCode = site.Code
		row := dbq.OpenRequestsRow{ID: req.ID, Number: req.Number, CreatedBy: int64(info.CreatedBy), RequiredBy: plainOf(req.Header["required_by"])}
		if st.Request, err = s.openRequest(ctx, tx, dbq.New(tx.SQL()), row); err != nil {
			return err
		}
		ids := make([]int64, len(st.Request.Rows))
		for i, r := range st.Request.Rows {
			ids[i] = r.RequestLineID
		}
		pending, err := PendingToOrder(ctx, tx, ids)
		if err != nil {
			return err
		}
		for i := range st.Request.Rows {
			st.Request.Rows[i].Pending = pending[st.Request.Rows[i].RequestLineID]
		}
		return s.fromRequestVendor(ctx, tx, p, vendorID, &st)
	})
	return st, err
}

// fromRequestVendor fills st's vendor from vendorID when p may read that
// party; an unknown or hidden one is left blank, so the picker shows nothing
// of it.
func (s *Service) fromRequestVendor(ctx context.Context, tx *db.Tx, p auth.Principal, vendorID int64, st *FromRequestState) error {
	if vendorID == 0 {
		return nil
	}
	pt, err := parties.Get(ctx, tx, vendorID)
	if errors.Is(err, doctype.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.d.Auth.CanTx(ctx, tx, p, party.DocType, auth.RightRead, &auth.RecordRef{DocType: party.DocType, ID: vendorID})
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	st.VendorCode, st.VendorName = pt.Code, pt.Name
	return nil
}
