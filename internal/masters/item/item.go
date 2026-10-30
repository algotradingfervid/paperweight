// Package item is the item master (MST Task 10): Goods or Service with its
// HSN/SAC code, stock and purchase UoMs, default tax code and the item's UoM
// conversion table (item_uom, its LinesTable).
package item

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item/dbq"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Item"

// Kind is the item master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Item", Table: "item", HasCode: true}

// ItemType is Goods or Service.
type ItemType string

// The two item types, in display order.
const (
	TypeGoods   ItemType = "Goods"
	TypeService ItemType = "Service"
)

const (
	msgTypeRequired     = "Item type is required (Goods or Service)."
	msgTypeUnknown      = "Item type must be Goods or Service."
	msgStockRequired    = "Stock UoM is required."
	msgServiceStock     = "Service items can't be stock items."
	msgStockLocked      = "Stock UoM can't change after the item is used. Create a new item."
	msgTypeLocked       = "Item type can't change after the item is used. Create a new item."
	msgUoMDisabled      = "UoM %s is disabled. Pick another UoM."
	MsgStockNotFound    = "Stock UoM not found. Pick one from the list."    // DOC-032's wording
	MsgPurchaseNotFound = "Purchase UoM not found. Pick one from the list." // DOC-032's wording
)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Item",
		PluralLabel: "Items",
		Table:       Kind.Table,
		LinesTable:  "item_uom",
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "item_type", Label: "Type", Column: "item_type", Type: doctype.Select, Required: true, Importable: true, ListPriority: 5,
				Options: []string{string(TypeGoods), string(TypeService)}},
			{Name: "stock_uom_id", Label: "UoM", Column: "stock_uom_id", Type: doctype.Link, Link: uom.DocType, Required: true, Importable: true, ListPriority: 6},
			{Name: "hsn_sac", Label: "HSN/SAC", Column: "hsn_sac", Type: doctype.Text, Importable: true, MaxLen: 8},
			{Name: "purchase_uom_id", Label: "Purchase UoM", Column: "purchase_uom_id", Type: doctype.Link, Link: uom.DocType},
			{Name: "default_tax_code_id", Label: "Tax Code", Column: "default_tax_code_id", Type: doctype.Link, Link: taxcode.DocType, Importable: true},
			{Name: "is_stock", Label: "Stock item", Column: "is_stock", Type: doctype.Check},
			{Name: "standard_rate", Label: "Standard Rate", Column: "standard_rate", Type: doctype.Rate, Importable: true, ListPriority: 2},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, ListPriority: 3, Filterable: true},
			{Name: "uom_id", Label: "UoM", Column: "uom_id", Type: doctype.Link, Link: uom.DocType, Required: true, OnLine: true},
			{Name: "factor", Label: "Conversion factor", Column: "factor", Type: doctype.Factor, Required: true, OnLine: true},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
	}, mstkit.StandardLookup(Kind))
}

// Item is one item.
type Item struct {
	ID                                          int64
	Code, Name                                  string
	Type                                        ItemType
	HSNSAC                                      string
	StockUoMID, PurchaseUoMID, DefaultTaxCodeID int64
	IsStock                                     bool
	StandardRate                                types.Rate
	Disabled                                    bool
	Version                                     int
}

// Input is what Create and Update accept. PurchaseUoMID 0 is the stock UoM;
// DefaultTaxCodeID 0 is none; StandardRate 0 is none (CD-37).
type Input struct {
	Code, Name                                  string
	Type                                        ItemType
	HSNSAC                                      string
	StockUoMID, PurchaseUoMID, DefaultTaxCodeID int64
	IsStock                                     bool
	StandardRate                                types.Rate
	Conversions                                 []Conversion
}

// Validate normalises code, name and HSN/SAC and checks the item type, the
// stock UoM (MST-069), the HSN or SAC (MST-070…MST-073), that a service isn't
// a stock item (MST-076) and the conversion factors (MST-080, MST-081). A
// blank purchase UoM becomes the stock UoM and a missing stock row is added
// first with factor 1 (MST-079, MST-083).
func Validate(in Input) (Input, error) {
	var es mstkit.Errors
	add := func(err error) {
		var ve *mstkit.ValidationError
		if errors.As(err, &ve) {
			for _, f := range ve.Fields {
				es.AddLine(f.Line, f.Field, f.Message)
			}
		}
	}
	out := in
	var err error
	out.Code, err = mstkit.NormalizeCode(in.Code)
	add(err)
	out.Name, err = mstkit.NormalizeName(in.Name)
	add(err)
	switch in.Type {
	case "":
		es.Add("item_type", msgTypeRequired)
	case TypeGoods:
		if out.HSNSAC, err = gstin.ValidateGoodsHSN(in.HSNSAC); err != nil {
			es.Add("hsn_sac", err.Error())
		}
	case TypeService:
		if out.HSNSAC, err = gstin.ValidateSAC(in.HSNSAC); err != nil {
			es.Add("hsn_sac", err.Error())
		}
		if in.IsStock {
			es.Add("is_stock", msgServiceStock)
		}
	default:
		es.Add("item_type", msgTypeUnknown)
	}
	if in.StockUoMID == 0 {
		es.Add("stock_uom_id", msgStockRequired)
	} else {
		if out.PurchaseUoMID == 0 {
			out.PurchaseUoMID = in.StockUoMID
		}
		out.Conversions = checkFactors(&es, in.StockUoMID, in.Conversions)
	}
	if err := es.Err(); err != nil {
		return Input{}, err
	}
	return out, nil
}

// Service writes and reads items.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Create adds an item and its conversion table.
func (s *Service) Create(ctx context.Context, p auth.Principal, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// CreateTx is Create inside tx.
func (s *Service) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLinks(ctx, tx, in, nil); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertItem(ctx, dbq.InsertItemParams{
		Code: in.Code, Name: in.Name, ItemType: string(in.Type), HsnSac: in.HSNSAC,
		StockUomID: in.StockUoMID, PurchaseUomID: in.PurchaseUoMID, DefaultTaxCodeID: nullID(in.DefaultTaxCodeID),
		IsStock: flag(in.IsStock), StandardRate: int64(in.StandardRate),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := writeConversions(ctx, tx, id, in.Conversions); err != nil {
		return mstkit.Saved{}, err
	}
	conv, err := s.conversionsText(ctx, tx, in.Conversions)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(in, conv)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the item and its conversion table.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx. A changed item type or stock UoM is refused
// once a document outside draft uses the item (MST-075, MST-090); otherwise a
// changed stock UoM resets the conversion table to [(stock UoM, 1)] with a
// warning (MST-089). The version is checked first, so a stale page gets the
// conflict, and the UPDATE is conditional on the posted version (CD-94).
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return mstkit.Saved{}, err
	}
	old, err := s.Get(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLocks(ctx, tx, old, in); err != nil {
		return mstkit.Saved{}, err
	}
	oldConv, err := s.Conversions(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	kept := oldConv // rows the item already has may use a UoM disabled since (MST-093)
	var warnings []string
	if in.StockUoMID != old.StockUoMID {
		kept = nil
		// The reset comes before Validate, so the dropped rows aren't checked.
		// The purchase UoM's row is gone too, so it falls back to the stock UoM.
		in.Conversions = []Conversion{{UoMID: in.StockUoMID, Factor: uom.FactorOne}}
		in.PurchaseUoMID = in.StockUoMID
		warnings = append(warnings, msgStockReset)
	}
	in, err = Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLinks(ctx, tx, in, kept); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	beforeConv, err := s.conversionsText(ctx, tx, oldConv)
	if err != nil {
		return mstkit.Saved{}, err
	}
	afterConv, err := s.conversionsText(ctx, tx, in.Conversions)
	if err != nil {
		return mstkit.Saved{}, err
	}
	before := auditFields(Input{
		Code: old.Code, Name: old.Name, Type: old.Type, HSNSAC: old.HSNSAC,
		StockUoMID: old.StockUoMID, PurchaseUoMID: old.PurchaseUoMID, DefaultTaxCodeID: old.DefaultTaxCodeID,
		IsStock: old.IsStock, StandardRate: old.StandardRate,
	}, beforeConv)
	after := auditFields(in, afterConv)
	if mstkit.Unchanged(before, after) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateItem(ctx, dbq.UpdateItemParams{
		Code: in.Code, Name: in.Name, ItemType: string(in.Type), HsnSac: in.HSNSAC,
		StockUomID: in.StockUoMID, PurchaseUomID: in.PurchaseUoMID, DefaultTaxCodeID: nullID(in.DefaultTaxCodeID),
		IsStock: flag(in.IsStock), StandardRate: int64(in.StandardRate),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		// CheckVersion passed inside BEGIN IMMEDIATE, so this can't happen.
		return mstkit.Saved{}, fmt.Errorf("update item %d: no row changed", id)
	}
	if err := writeConversions(ctx, tx, id, in.Conversions); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, after); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1, Warnings: warnings}, nil
}

// checkLocks refuses a changed item type or stock UoM on an item in use. A
// blank or unknown type is left to Validate.
func (s *Service) checkLocks(ctx context.Context, tx *db.Tx, old Item, in Input) error {
	stock := in.StockUoMID != old.StockUoMID
	kind := in.Type != old.Type && (in.Type == TypeGoods || in.Type == TypeService)
	if !stock && !kind {
		return nil
	}
	_, used, err := s.env.UsedOutsideDrafts(ctx, tx, Kind.Table, old.ID)
	if err != nil || !used {
		return err
	}
	var es mstkit.Errors
	if kind {
		es.Add("item_type", msgTypeLocked)
	}
	if stock {
		es.Add("stock_uom_id", msgStockLocked)
	}
	return es.Err()
}

// checkLinks checks, inside tx, that the stock and purchase UoMs and the
// default tax code exist and the tax code is enabled (MST-074, CD-94), then
// the conversion rows' UoMs (MST-082, MST-083). A UoM with no row in kept is
// new to the item and must be enabled (MST-093); the purchase UoM always has
// a row. The foreign keys are only a backstop.
func (s *Service) checkLinks(ctx context.Context, tx *db.Tx, in Input, kept []Conversion) error {
	var es mstkit.Errors
	found := map[int64]uom.UoM{}
	uoms := uom.New(s.env)
	lookup := func(id int64) (uom.UoM, bool, error) {
		if u, ok := found[id]; ok {
			return u, true, nil
		}
		u, err := uoms.Get(ctx, tx, id)
		if errors.Is(err, doctype.ErrNotFound) {
			return uom.UoM{}, false, nil
		}
		if err != nil {
			return uom.UoM{}, false, err
		}
		found[id] = u
		return u, true, nil
	}
	stock, stockOK, err := lookup(in.StockUoMID)
	if err != nil {
		return err
	}
	switch {
	case !stockOK:
		es.Add("stock_uom_id", MsgStockNotFound)
	case stock.Disabled && !hasRow(kept, in.StockUoMID):
		es.Add("stock_uom_id", fmt.Sprintf(msgUoMDisabled, stock.Code))
	}
	purchase, purchaseOK, err := lookup(in.PurchaseUoMID)
	if err != nil {
		return err
	}
	if !purchaseOK {
		es.Add("purchase_uom_id", MsgPurchaseNotFound)
	}
	if err := taxcode.New(s.env).CheckEnabledTx(ctx, tx, in.DefaultTaxCodeID); err != nil {
		var ve *mstkit.ValidationError
		if !errors.As(err, &ve) {
			return err
		}
		for _, f := range ve.Fields {
			es.Add(f.Field, f.Message)
		}
	}
	if err := checkRows(&es, in, kept, lookup); err != nil {
		return err
	}
	if stockOK && purchaseOK && !hasRow(in.Conversions, in.PurchaseUoMID) {
		es.Add("purchase_uom_id", fmt.Sprintf(msgPurchaseNoRow, purchase.Code, purchase.Code, stock.Code))
	}
	return es.Err()
}

// SetDisabled disables or enables the item.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx.
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// Delete deletes the item and its conversion rows unless it is referenced.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// DeleteTx is Delete inside tx.
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightDelete, ref(id)); err != nil {
		return err
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version)
}

// Get reads the item; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Item, error) {
	r, err := dbq.New(tx.SQL()).GetItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, doctype.ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return fromRow(dbq.GetItemByCodeRow(r)), nil
}

// ByCode reads the item with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Item, bool, error) {
	r, err := dbq.New(tx.SQL()).GetItemByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, err
	}
	return fromRow(r), true, nil
}

func fromRow(r dbq.GetItemByCodeRow) Item {
	return Item{
		ID: r.ID, Code: r.Code, Name: r.Name, Type: ItemType(r.ItemType), HSNSAC: r.HsnSac,
		StockUoMID: r.StockUomID, PurchaseUoMID: r.PurchaseUomID, DefaultTaxCodeID: r.DefaultTaxCodeID.Int64,
		IsStock: r.IsStock != 0, StandardRate: types.Rate(r.StandardRate),
		Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

// conversionsText is rows as audited: "UoM code:factor, …".
func (s *Service) conversionsText(ctx context.Context, tx *db.Tx, rows []Conversion) (string, error) {
	uoms := uom.New(s.env)
	out := make([]string, len(rows))
	for i, c := range rows {
		u, err := uoms.Get(ctx, tx, c.UoMID)
		if err != nil {
			return "", err
		}
		out[i] = u.Code + ":" + c.Factor.Plain()
	}
	return strings.Join(out, ", "), nil
}

// auditFields is the item as audited, with its conversions as conv.
func auditFields(in Input, conv string) map[string]string {
	id := func(v int64) string { return strconv.FormatInt(v, 10) }
	return map[string]string{
		"code": in.Code, "name": in.Name, "item_type": string(in.Type), "hsn_sac": in.HSNSAC,
		"stock_uom_id": id(in.StockUoMID), "purchase_uom_id": id(in.PurchaseUoMID),
		"default_tax_code_id": id(in.DefaultTaxCodeID), "is_stock": strconv.FormatBool(in.IsStock),
		"standard_rate": in.StandardRate.Plain(), "conversions": conv,
	}
}
