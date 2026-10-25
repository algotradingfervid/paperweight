package gstcalc

import "github.com/vardhan/internaltools/internal/types"

// TaxType is the kind of GST a bill charges.
type TaxType string

const (
	TaxCGSTSGST TaxType = "cgst_sgst"
	TaxIGST     TaxType = "igst"
)

// stateNames is the GST state table: India Compliance's STATE_NUMBERS
// (D-MST-10), without "Other Countries" (96). Legacy codes 25 (Daman and Diu)
// and 28 (old Andhra Pradesh) are not current GST state codes.
var stateNames = map[string]string{
	"01": "Jammu and Kashmir",
	"02": "Himachal Pradesh",
	"03": "Punjab",
	"04": "Chandigarh",
	"05": "Uttarakhand",
	"06": "Haryana",
	"07": "Delhi",
	"08": "Rajasthan",
	"09": "Uttar Pradesh",
	"10": "Bihar",
	"11": "Sikkim",
	"12": "Arunachal Pradesh",
	"13": "Nagaland",
	"14": "Manipur",
	"15": "Mizoram",
	"16": "Tripura",
	"17": "Meghalaya",
	"18": "Assam",
	"19": "West Bengal",
	"20": "Jharkhand",
	"21": "Odisha",
	"22": "Chhattisgarh",
	"23": "Madhya Pradesh",
	"24": "Gujarat",
	"26": "Dadra and Nagar Haveli and Daman and Diu",
	"27": "Maharashtra",
	"29": "Karnataka",
	"30": "Goa",
	"31": "Lakshadweep Islands",
	"32": "Kerala",
	"33": "Tamil Nadu",
	"34": "Puducherry",
	"35": "Andaman and Nicobar Islands",
	"36": "Telangana",
	"37": "Andhra Pradesh",
	"38": "Ladakh",
	"97": "Other Territory",
}

// SupplierState returns the supplier's GST state code: the GSTIN's first two
// digits when it has a GSTIN, otherwise the state on its address.
func SupplierState(gstin, addressState string) (string, error) {
	if s := placeState(gstin, addressState); s != "" {
		return s, nil
	}
	return "", &types.InputError{Msg: "Set the state on the supplier's address so GST can be worked out."}
}

// BranchState returns the branch's GST state code by the same rule as SupplierState.
func BranchState(branchName, gstin, state string) (string, error) {
	if s := placeState(gstin, state); s != "" {
		return s, nil
	}
	return "", &types.InputError{Msg: "Set the state for branch " + branchName + " so GST can be worked out."}
}

func placeState(gstin, state string) string {
	if len(gstin) >= 2 {
		return gstin[:2]
	}
	return state
}

// CheckTaxType rejects IGST on an intra-state bill and CGST/SGST on an inter-state one.
func CheckTaxType(interState bool, t TaxType, supplierState, branchState string) error {
	switch {
	case !interState && t == TaxIGST:
		return &types.InputError{Msg: "This bill is intra-state (supplier and branch are both in " +
			stateLabel(branchState) + "), so use CGST and SGST, not IGST."}
	case interState && t == TaxCGSTSGST:
		return &types.InputError{Msg: "This bill is inter-state (" + stateLabel(supplierState) + " to " +
			stateLabel(branchState) + "), so use IGST, not CGST and SGST."}
	}
	return nil
}

// stateLabel returns the state's name, or the code when the table doesn't know it.
func stateLabel(code string) string {
	if name, ok := StateName(code); ok {
		return name
	}
	return code
}
