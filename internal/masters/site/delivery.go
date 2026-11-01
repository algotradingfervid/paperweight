package site

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/site/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// M0-58 (CD-337 (1)): a site's optional delivery address and contact.
const (
	msgDeliveryLine1 = "Address line 1 is required."
	msgDeliveryCity  = "City is required."
	msgDeliveryState = "State is required for the delivery address."
	msgContactName   = "Contact name is required."
	// MsgDeliveryInState is the refusal of a delivery address outside the
	// site's branch's state: sites are in their branch's state (CD-234).
	MsgDeliveryInState = "Delivery address state must match the branch state."
)

// Delivery is a site's optional delivery address and contact, stored as an
// address and a contact the site owns (CD-337 (1)). All address fields blank
// is no address; name and mobile blank is no contact. The address is always
// in India, in the site's branch's state.
type Delivery struct {
	Line1, Line2, City, StateCode, Pincode string
	ContactName, ContactMobile             string // the mobile as +91 and ten digits
}

// HasAddress reports whether d has an address.
func (d Delivery) HasAddress() bool {
	return d.Line1 != "" || d.Line2 != "" || d.City != "" || d.StateCode != "" || d.Pincode != ""
}

// HasContact reports whether d has a contact.
func (d Delivery) HasContact() bool { return d.ContactName != "" || d.ContactMobile != "" }

// Text is the address as documents print it (address.DisplayText), e.g.
// "Gate 2\nPune - 411057\nMaharashtra (27)"; "" with no address.
func (d Delivery) Text() string {
	return address.DisplayText(address.Address{
		Line1: d.Line1, Line2: d.Line2, City: d.City, StateCode: d.StateCode, Pincode: d.Pincode, Country: "India",
	})
}

// DeliveryAddress is the site's delivery address and contact for a module's
// page or print ("Deliver to", CD-337 (1)), read in the caller's tx; ok is
// false when the site has neither. It checks no right, so call it only with a
// site id stored on a document the caller has already read-checked, never with
// a posted id (CD-356 (2), CD-217/228).
func DeliveryAddress(ctx context.Context, tx *db.Tx, siteID int64) (Delivery, bool, error) {
	d, err := readDelivery(ctx, dbq.New(tx.SQL()), siteID)
	return d, d != Delivery{}, err
}

// readDelivery reads the site's address and contact.
func readDelivery(ctx context.Context, q *dbq.Queries, siteID int64) (Delivery, error) {
	var d Delivery
	a, err := q.SiteAddress(ctx, nullID(siteID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Delivery{}, err
	default:
		d.Line1, d.Line2, d.City, d.StateCode, d.Pincode = a.Line1, a.Line2, a.City, a.StateCode.String, a.Pincode
	}
	c, err := q.SiteContact(ctx, nullID(siteID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Delivery{}, err
	default:
		d.ContactName, d.ContactMobile = c.Name, c.Mobile
	}
	return d, nil
}

// validateDelivery normalises d and adds its refusals to es: an address needs
// line 1, a city and an Indian state, its pincode is checked against the state
// as Address does (MST-055, MST-056); a contact needs a name, and its mobile
// is an Indian mobile (MST-064).
func validateDelivery(d Delivery, es *mstkit.Errors) Delivery {
	trim := strings.TrimSpace
	d = Delivery{
		Line1: trim(d.Line1), Line2: trim(d.Line2), City: trim(d.City), StateCode: trim(d.StateCode), Pincode: trim(d.Pincode),
		ContactName: trim(d.ContactName), ContactMobile: trim(d.ContactMobile),
	}
	if d.HasAddress() {
		if d.Line1 == "" {
			es.Add("delivery_line1", msgDeliveryLine1)
		}
		if d.City == "" {
			es.Add("delivery_city", msgDeliveryCity)
		}
		es.CheckLen("delivery_line1", "Address line 1", d.Line1)
		es.CheckLen("delivery_line2", "Address line 2", d.Line2)
		es.CheckLen("delivery_city", "City", d.City)
		if !gstin.IsIndianState(d.StateCode) {
			es.Add("delivery_state_code", msgDeliveryState)
		}
		pin, err := gstin.ValidatePincode(d.Pincode)
		if err == nil {
			err = gstin.PincodeInState(pin, d.StateCode)
		}
		if err != nil {
			es.Add("delivery_pincode", err.Error())
		}
		d.Pincode = pin
	}
	if d.HasContact() {
		if d.ContactName == "" {
			es.Add("contact_name", msgContactName)
		}
		es.CheckLen("contact_name", "Contact name", d.ContactName)
		if d.ContactMobile != "" {
			mobile, err := address.NormalizeMobile(d.ContactMobile)
			if err != nil {
				es.Add("contact_mobile", err.Error())
			}
			d.ContactMobile = mobile
		}
	}
	return d
}

// saveDelivery replaces the site's address and contact with d's, when they
// changed from old.
func (s *Service) saveDelivery(ctx context.Context, tx *db.Tx, p auth.Principal, siteID int64, old, d Delivery) error {
	if old == d {
		return nil
	}
	q := dbq.New(tx.SQL())
	if err := q.DeleteSiteAddress(ctx, nullID(siteID)); err != nil {
		return err
	}
	if err := q.DeleteSiteContact(ctx, nullID(siteID)); err != nil {
		return err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	if d.HasAddress() {
		if err := q.InsertSiteAddress(ctx, dbq.InsertSiteAddressParams{
			SiteID: nullID(siteID), Name: d.City, Line1: d.Line1, Line2: d.Line2, City: d.City,
			StateCode: sql.NullString{String: d.StateCode, Valid: true}, Pincode: d.Pincode,
			CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
		}); err != nil {
			return err
		}
	}
	if !d.HasContact() {
		return nil
	}
	id, err := q.InsertSiteContact(ctx, dbq.InsertSiteContactParams{
		SiteID: nullID(siteID), Name: d.ContactName, FirstName: d.ContactName,
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil || d.ContactMobile == "" {
		return err
	}
	return q.InsertSiteContactMobile(ctx, dbq.InsertSiteContactMobileParams{ContactID: id, Number: d.ContactMobile})
}
