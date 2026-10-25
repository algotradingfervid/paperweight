package testkit

import "github.com/vardhan/internaltools/internal/types"

// The values in this file are copied verbatim from
// docs/tests/core/00-fixtures.md (canonical test fixtures). Change that
// document first, then this file.

// Fixtures holds every canonical fixture value. Seed fills in the database
// ids of employees, projects and sites.
type Fixtures struct {
	Company       FixtureCompany
	Branches      []FixtureBranch
	Parties       []FixtureParty
	InvalidGSTIN  string // invalid-checksum GSTIN used in negative tests
	Items         []FixtureItem
	Units         []FixtureUnit
	TaxCodes      []FixtureTaxCode
	Projects      []FixtureProject
	Sites         []FixtureSite
	ExpenseTypes  []string
	Roles         []string // canonical roles
	Users         []FixtureUser
	Password      string // test password for all fixture users
	Restrictions  []FixtureRestriction
	Employees     []FixtureEmployee
	Rights        []FixtureRight
	ApprovalRules []FixtureApprovalRule
	Numbering     []FixtureNumbering
	ReferencePO   FixtureReferencePO
}

type FixtureCompany struct {
	LegalName, PAN string
}

type FixtureBranch struct {
	Code, Name, State, StateCode, GSTIN string
	HeadOffice                          bool
}

type FixtureParty struct {
	Code, Name, PAN, GSTCategory string // PAN "" when the party has none
	Addresses                    []FixtureAddress
}

type FixtureAddress struct {
	Place, GSTIN   string // GSTIN "" when unregistered
	PrimaryBilling bool
}

type FixtureItem struct {
	Code, Name, Type, HSN string // HSN holds the SAC for services
	StockUoM              string // display label as in 00-fixtures.md (kg, Nos); the unit code is in Units
	StockWhole            bool   // stock UoM is whole-number
	OtherUoM              string // display label as in 00-fixtures.md (Box, Drum); the unit code is in Units; "" when none
	OtherFactor           int64  // stock units per OtherUoM
	GST                   string // as written in the fixtures doc
}

type FixtureUnit struct {
	Code, UQC string
	Whole     bool
}

type FixtureTaxCode struct {
	Category string // Taxable, Nil-Rated, Exempted, Non-GST
	Percent  types.Percent
}

type FixtureProject struct {
	ID         int64
	Code, Name string
}

type FixtureSite struct {
	ID                     int64
	Code, Kind, BranchCode string // Kind is warehouse or site
	ProjectCode            string // "" when none
	ProjectID              int64  // 0 when none
}

type FixtureUser struct {
	ID           types.UserID
	Email, Name  string
	Roles        []string
	EmployeeCode string
	Disabled     bool
}

// FixtureRestriction limits a user to the listed project and site codes.
type FixtureRestriction struct {
	Email                   string
	ProjectCodes, SiteCodes []string
}

type FixtureEmployee struct {
	ID                        int64
	Code, ManagerCode, Status string // ManagerCode "" when none
	UserID                    types.UserID
}

// FixtureRight is one row of the seeded rights matrix. DocType "*" is every
// DocType (CD-86); "every master" is left for WP2 to map onto its masters.
type FixtureRight struct {
	Role, DocType string
	Rights        []string
	Scope         string // own, team or all (CD-11)
}

type FixtureApprovalRule struct {
	DocType      string
	FromAmount   types.Money
	Approver     string // "role <Role>" or "reporting manager"
	FallbackRole string // "" when none
	SelfApproval bool
}

type FixtureNumbering struct {
	DocType, Template, FirstNumber string
}

type FixtureReferencePO struct {
	Party, PartyAddress, Branch string
	Lines                       []FixturePOLine
	Taxable, CGST, SGST         types.Money
	GrandTotal, RoundedTotal    types.Money
	InWords                     string
}

type FixturePOLine struct {
	Line   int
	Item   string
	Qty    types.Qty
	UoM    string
	Rate   types.Rate
	Amount types.Money
}

var allRights = []string{"read", "create", "write", "submit", "cancel", "amend", "delete", "approve", "print", "export", "import"}

func canonical() Fixtures {
	return Fixtures{
		Company: FixtureCompany{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"},
		Branches: []FixtureBranch{
			{Code: "PUNE", Name: "Pune Works", State: "Maharashtra", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", HeadOffice: true},
			{Code: "BLR", Name: "Bengaluru Site Office", State: "Karnataka", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"},
		},
		Parties: []FixtureParty{
			{Code: "SHREE", Name: "Shree Steel Traders", PAN: "AABCS1429B", GSTCategory: "Registered Regular", Addresses: []FixtureAddress{
				{Place: "Mumbai", GSTIN: "27AABCS1429B1ZU", PrimaryBilling: true},
				{Place: "Thane", GSTIN: "27AABCS1429B2ZT"},
				{Place: "Delhi", GSTIN: "07AABCS1429B1ZW"},
			}},
			{Code: "BHARAT", Name: "Bharat Tools", PAN: "AABCT1332L", GSTCategory: "Registered Regular", Addresses: []FixtureAddress{
				{Place: "Bengaluru", GSTIN: "29AABCT1332L1ZA"},
			}},
			{Code: "RAJKOT", Name: "Rajkot Castings", PAN: "AAACR5055K", GSTCategory: "Registered Regular", Addresses: []FixtureAddress{
				{Place: "Rajkot", GSTIN: "24AAACR5055K1ZD"},
			}},
			{Code: "COROM", Name: "Coromandel Cables", PAN: "AAACT2727Q", GSTCategory: "Registered Regular", Addresses: []FixtureAddress{
				{Place: "Chennai", GSTIN: "33AAACT2727Q1Z3"},
			}},
			{Code: "LOCAL", Name: "Ganesh Hardware (unregistered)", GSTCategory: "Unregistered", Addresses: []FixtureAddress{
				{Place: "Pune"},
			}},
		},
		InvalidGSTIN: "27AABCS1429B1Z5",
		Items: []FixtureItem{
			{Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5", Type: "Goods", HSN: "7216", StockUoM: "kg", GST: "18%"},
			{Code: "BOLT-M12", Name: "Hex Bolt M12x60", Type: "Goods", HSN: "7318", StockUoM: "Nos", StockWhole: true, OtherUoM: "Box", OtherFactor: 100, GST: "18%"},
			{Code: "WIRE-18G", Name: "Binding Wire 18G", Type: "Goods", HSN: "7217", StockUoM: "kg", GST: "18%"},
			{Code: "CABLE-4C", Name: "Armoured Cable 4 core 16 sq mm", Type: "Goods", HSN: "8544", StockUoM: "m", OtherUoM: "Drum", OtherFactor: 500, GST: "18%"},
			{Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade", Type: "Goods", HSN: "2523", StockUoM: "Bag", StockWhole: true, GST: "28% (older documents) / 18%"},
			{Code: "SVC-CRANE", Name: "Crane hire", Type: "Service", HSN: "997313", StockUoM: "Hour", GST: "18%"},
		},
		// The full seeded list (MST-078); HRS uses UQC OTH, DRUM uses DRM.
		Units: []FixtureUnit{
			{Code: "NOS", UQC: "NOS", Whole: true},
			{Code: "KGS", UQC: "KGS"},
			{Code: "MTR", UQC: "MTR"},
			{Code: "LTR", UQC: "LTR"},
			{Code: "BOX", UQC: "BOX", Whole: true},
			{Code: "SET", UQC: "SET", Whole: true},
			{Code: "BAG", UQC: "BAG", Whole: true},
			{Code: "TON", UQC: "TON"},
			{Code: "SQM", UQC: "SQM"},
			{Code: "HRS", UQC: "OTH"},
			{Code: "DRUM", UQC: "DRM", Whole: true},
		},
		// Percent values are in millipercent (18% = 18000).
		TaxCodes: []FixtureTaxCode{
			{Category: "Taxable", Percent: 250},
			{Category: "Taxable", Percent: 3000},
			{Category: "Taxable", Percent: 5000},
			{Category: "Taxable", Percent: 12000},
			{Category: "Taxable", Percent: 18000},
			{Category: "Taxable", Percent: 28000},
			{Category: "Taxable", Percent: 40000},
			{Category: "Nil-Rated", Percent: 0},
			{Category: "Exempted", Percent: 0},
			{Category: "Non-GST", Percent: 0},
		},
		Projects: []FixtureProject{
			{Code: "PRJ-METRO", Name: "Pune Metro Depot"},
			{Code: "PRJ-TECHPARK", Name: "Whitefield Tech Park, Bengaluru"},
		},
		Sites: []FixtureSite{
			{Code: "PUNE-WH", Kind: "warehouse", BranchCode: "PUNE"},
			{Code: "METRO-SITE", Kind: "site", BranchCode: "PUNE", ProjectCode: "PRJ-METRO"},
			{Code: "TECH-SITE", Kind: "site", BranchCode: "BLR", ProjectCode: "PRJ-TECHPARK"},
		},
		ExpenseTypes: []string{"TRAVEL", "FOOD", "FUEL", "LODGING", "SITE-MISC"},
		Roles:        []string{"Administrator", "Director", "Purchase Manager", "Purchase User", "Stores", "Masters Manager", "Employee"},
		Users: []FixtureUser{
			{ID: 1, Email: "admin@vardhan.test", Name: "Anil Kapoor", Roles: []string{"Administrator"}, EmployeeCode: "EMP-001"},
			{ID: 2, Email: "meena@vardhan.test", Name: "Meena Iyer", Roles: []string{"Director"}, EmployeeCode: "EMP-002"},
			{ID: 3, Email: "ravi@vardhan.test", Name: "Ravi Kulkarni", Roles: []string{"Purchase Manager", "Employee"}, EmployeeCode: "EMP-003"},
			{ID: 4, Email: "priya@vardhan.test", Name: "Priya Shah", Roles: []string{"Purchase User", "Employee"}, EmployeeCode: "EMP-004"},
			{ID: 5, Email: "suresh@vardhan.test", Name: "Suresh Patil", Roles: []string{"Stores", "Employee"}, EmployeeCode: "EMP-005"},
			{ID: 6, Email: "kavya@vardhan.test", Name: "Kavya Rao", Roles: []string{"Masters Manager", "Employee"}, EmployeeCode: "EMP-006"},
			{ID: 7, Email: "deepak@vardhan.test", Name: "Deepak Nair", Roles: []string{"Employee"}, EmployeeCode: "EMP-007"},
			{ID: 8, Email: "old@vardhan.test", Name: "Former Staff", Roles: []string{"Employee"}, EmployeeCode: "EMP-008", Disabled: true},
		},
		Password: "Correct-Horse-42",
		Restrictions: []FixtureRestriction{
			{Email: "priya@vardhan.test", ProjectCodes: []string{"PRJ-METRO"}},
			{Email: "suresh@vardhan.test", SiteCodes: []string{"PUNE-WH", "METRO-SITE"}},
		},
		Employees: []FixtureEmployee{
			{Code: "EMP-001", Status: "Active", UserID: 1},
			{Code: "EMP-002", Status: "Active", UserID: 2},
			{Code: "EMP-003", ManagerCode: "EMP-002", Status: "Active", UserID: 3},
			{Code: "EMP-004", ManagerCode: "EMP-003", Status: "Active", UserID: 4},
			{Code: "EMP-005", ManagerCode: "EMP-003", Status: "Active", UserID: 5},
			{Code: "EMP-006", ManagerCode: "EMP-002", Status: "Active", UserID: 6},
			{Code: "EMP-007", ManagerCode: "EMP-004", Status: "Active", UserID: 7},
			{Code: "EMP-008", Status: "Left", UserID: 8},
		},
		// "Purchase Order / fixture.Order" rows become one row per DocType.
		Rights: []FixtureRight{
			{Role: "Administrator", DocType: "*", Rights: allRights, Scope: "all"},
			{Role: "Purchase Manager", DocType: "Purchase Order", Rights: []string{"read", "approve", "cancel", "amend", "print", "export"}, Scope: "all"},
			{Role: "Purchase Manager", DocType: "fixture.Order", Rights: []string{"read", "approve", "cancel", "amend", "print", "export"}, Scope: "all"},
			{Role: "Purchase User", DocType: "Purchase Order", Rights: []string{"read", "create", "write", "submit", "print", "export"}, Scope: "all"},
			{Role: "Purchase User", DocType: "Purchase Order", Rights: []string{"delete"}, Scope: "own"},
			{Role: "Purchase User", DocType: "fixture.Order", Rights: []string{"read", "create", "write", "submit", "print", "export"}, Scope: "all"},
			{Role: "Purchase User", DocType: "fixture.Order", Rights: []string{"delete"}, Scope: "own"},
			{Role: "Stores", DocType: "GRN", Rights: []string{"read", "create", "write", "submit", "print"}, Scope: "all"},
			{Role: "Stores", DocType: "fixture.Receipt", Rights: []string{"read", "create", "write", "submit", "print"}, Scope: "all"},
			{Role: "Stores", DocType: "Purchase Order", Rights: []string{"read"}, Scope: "all"},
			{Role: "Stores", DocType: "fixture.Order", Rights: []string{"read"}, Scope: "all"},
			{Role: "Employee", DocType: "Expense Claim", Rights: []string{"read", "create", "write", "submit", "print"}, Scope: "own"},
			{Role: "Employee", DocType: "Expense Claim", Rights: []string{"read", "approve"}, Scope: "team"},
			{Role: "Employee", DocType: "fixture.Claim", Rights: []string{"read", "approve"}, Scope: "team"},
			{Role: "Director", DocType: "Purchase Order", Rights: []string{"read", "approve", "print", "export"}, Scope: "all"},
			{Role: "Director", DocType: "fixture.Order", Rights: []string{"read", "approve", "print", "export"}, Scope: "all"},
			{Role: "Director", DocType: "Expense Claim", Rights: []string{"read", "approve", "print", "export"}, Scope: "all"},
			{Role: "Director", DocType: "fixture.Claim", Rights: []string{"read", "approve", "print", "export"}, Scope: "all"},
			{Role: "Masters Manager", DocType: "every master", Rights: []string{"read", "create", "write", "delete", "export", "import"}, Scope: "all"},
		},
		ApprovalRules: []FixtureApprovalRule{
			{DocType: "fixture.Order", FromAmount: 50_000_00, Approver: "role Purchase Manager"},
			{DocType: "Purchase Order", FromAmount: 50_000_00, Approver: "role Purchase Manager"},
			{DocType: "fixture.Order", FromAmount: 5_00_000_00, Approver: "role Director"},
			{DocType: "Purchase Order", FromAmount: 5_00_000_00, Approver: "role Director"},
			{DocType: "Expense Claim", FromAmount: 0, Approver: "reporting manager", FallbackRole: "Director"},
			{DocType: "fixture.Claim", FromAmount: 0, Approver: "reporting manager", FallbackRole: "Director"},
		},
		Numbering: []FixtureNumbering{
			{DocType: "fixture.Order", Template: "PO/{FY}/{#####}", FirstNumber: "PO/26-27/00001"},
			{DocType: "Purchase Order", Template: "PO/{FY}/{#####}", FirstNumber: "PO/26-27/00001"},
			{DocType: "fixture.Receipt", Template: "GRN/{FY}/{#####}", FirstNumber: "GRN/26-27/00001"},
			{DocType: "GRN", Template: "GRN/{FY}/{#####}", FirstNumber: "GRN/26-27/00001"},
			{DocType: "fixture.Claim", Template: "EXP/{FY}/{#####}", FirstNumber: "EXP/26-27/00001"},
			{DocType: "Expense Claim", Template: "EXP/{FY}/{#####}", FirstNumber: "EXP/26-27/00001"},
		},
		// Money in paise, Qty in thousandths, Rate in ten-thousandths of a rupee.
		ReferencePO: FixtureReferencePO{
			Party: "SHREE", PartyAddress: "Mumbai", Branch: "PUNE",
			Lines: []FixturePOLine{
				{Line: 1, Item: "MS-ANGLE-50", Qty: 1_200_000, UoM: "kg", Rate: 62_5000, Amount: 75_000_00}, // 1,200.000 kg × ₹62.5000 = ₹75,000.00
				{Line: 2, Item: "WIRE-18G", Qty: 50_000, UoM: "kg", Rate: 96_0000, Amount: 4_800_00},        // 50.000 kg × ₹96.0000 = ₹4,800.00
			},
			Taxable:      79_800_00, // ₹79,800.00
			CGST:         7_182_00,  // 9%
			SGST:         7_182_00,  // 9%
			GrandTotal:   94_164_00,
			RoundedTotal: 94_164_00,
			InWords:      "Rupees Ninety Four Thousand One Hundred Sixty Four Only",
		},
	}
}
