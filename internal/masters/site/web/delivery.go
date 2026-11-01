package siteweb

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// The delivery form's fields (M0-58, CD-337 (1)), named as the service's
// field errors.
var deliveryFields = []doctype.Field{
	{Name: "delivery_line1", Label: "Address line 1", Type: doctype.Text, Hint: "Leave every field blank to remove the address and contact."},
	{Name: "delivery_line2", Label: "Address line 2", Type: doctype.Text},
	{Name: "delivery_city", Label: "City", Type: doctype.Text},
	{Name: "delivery_state_code", Label: "State", Type: doctype.Text, Hint: "The branch's state."},
	{Name: "delivery_pincode", Label: "Pincode", Type: doctype.Text},
	{Name: "contact_name", Label: "Contact name", Type: doctype.Text},
	{Name: "contact_mobile", Label: "Contact mobile", Type: doctype.Text},
}

// stateOptions is the delivery state select: GST states by name.
func stateOptions() []components.Option {
	opts := []components.Option{{Label: "Choose a state"}}
	for _, s := range gstin.States() {
		opts = append(opts, components.Option{Value: s.Code, Label: s.Name + " (" + s.Code + ")"})
	}
	return opts
}

func recordPath(id int64) string { return "/m/site/" + strconv.FormatInt(id, 10) }

func deliveryPath(id int64) string { return recordPath(id) + "/delivery" }

// delivery is the site's delivery address pages.
type delivery struct {
	svc *site.Service
	env mstkit.Env
}

// card is the record page's delivery address and contact, with Change for a
// user who may write the site.
func (d delivery) card(s site.Site) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		p, _ := auth.PrincipalFrom(ctx)
		var change *components.Link
		if d.env.CanRow(ctx, p, site.Kind, auth.RightWrite, ref(s.ID)) == nil {
			change = &components.Link{Label: "Change", Href: deliveryPath(s.ID)}
		}
		var pairs []components.Pair
		if s.Delivery.HasAddress() {
			pairs = append(pairs, components.Pair{Label: "Address", Value: s.Delivery.Text()})
		}
		if s.Delivery.ContactName != "" {
			pairs = append(pairs, components.Pair{Label: "Contact", Value: s.Delivery.ContactName})
		}
		if s.Delivery.ContactMobile != "" {
			pairs = append(pairs, components.Pair{Label: "Mobile", Value: s.Delivery.ContactMobile})
		}
		return deliveryCard(pairs, change).Render(ctx, w)
	})
}

// routes mounts the delivery form at /m/site/{id}/delivery.
func (d delivery) routes(pages mstweb.Pages) func(chi.Router) {
	return func(r chi.Router) {
		r.Get("/m/site/{id:[1-9][0-9]*}/delivery", pages.Handle(d.page(pages)))
		r.Post("/m/site/{id:[1-9][0-9]*}/delivery", pages.Handle(d.save(pages)))
	}
}

// open reads the site the URL names, refusing a user who may not write it as
// its record page does.
func (d delivery) open(r *http.Request, p auth.Principal) (site.Site, error) {
	ctx := r.Context()
	id := mstweb.RecordID(r)
	var s site.Site
	err := d.env.DB.Read(ctx, func(tx *db.Tx) error {
		if err := d.env.CanRowTx(ctx, tx, p, site.Kind, auth.RightWrite, ref(id)); err != nil {
			return err
		}
		var err error
		s, err = d.svc.Get(ctx, tx, id)
		return err
	})
	return s, err
}

func (d delivery) show(w http.ResponseWriter, r *http.Request, pages mstweb.Pages, status int, s site.Site, f mstweb.Form) error {
	sub := mstweb.SubForm{
		ID: s.ID, Title: "Delivery address", Action: deliveryPath(s.ID), Form: f,
		Back: components.Link{Label: s.Code + " " + s.Name, Href: recordPath(s.ID)},
	}
	for _, fd := range deliveryFields {
		sub.Inputs = append(sub.Inputs, pages.Input(fd, fd.Name, f))
		sub.Required = sub.Required || fd.Required
	}
	mstweb.RenderSubForm(w, r, status, sub)
	return nil
}

func (d delivery) page(pages mstweb.Pages) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		s, err := d.open(r, p)
		if err != nil {
			return err
		}
		v := s.Delivery
		values := map[string]string{
			"delivery_line1": v.Line1, "delivery_line2": v.Line2, "delivery_city": v.City, "delivery_state_code": v.StateCode,
			"delivery_pincode": v.Pincode, "contact_name": v.ContactName, "contact_mobile": v.ContactMobile,
		}
		return d.show(w, r, pages, http.StatusOK, s, mstweb.Form{Values: values, Version: s.Version})
	}
}

// save is the site's Update at the posted version (CD-94) with its stored
// fields and the posted delivery, so every site rule runs on it.
func (d delivery) save(pages mstweb.Pages) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		s, err := d.open(r, p)
		if err != nil {
			return err
		}
		f := mstweb.Form{Values: map[string]string{}, Version: version}
		names := make([]string, len(deliveryFields))
		for i, fd := range deliveryFields {
			f.Values[fd.Name], names[i] = r.PostFormValue(fd.Name), fd.Name
		}
		v := f.Values
		in := site.Input{
			Code: s.Code, Name: s.Name, BranchID: s.BranchID, ProjectID: s.ProjectID, Type: s.Type,
			Delivery: &site.Delivery{
				Line1: v["delivery_line1"], Line2: v["delivery_line2"], City: v["delivery_city"], StateCode: v["delivery_state_code"],
				Pincode: v["delivery_pincode"], ContactName: v["contact_name"], ContactMobile: v["contact_mobile"],
			},
		}
		if _, err := d.svc.Update(r.Context(), p, s.ID, version, in); err != nil {
			f, status, ok := f.Refused(err, names)
			if !ok {
				return err
			}
			return d.show(w, r, pages, status, s, f)
		}
		return mstweb.Done(w, r, "Site saved.", recordPath(s.ID))
	}
}
