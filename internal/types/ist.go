package types

import (
	"time"
	_ "time/tzdata" // embed the zone database so IST loads without system files (D-NUM-18)
)

// IST is Asia/Kolkata; "today" and "this FY" are taken in it (D-NUM-18).
var IST = mustLoad("Asia/Kolkata")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic("types: load " + name + ": " + err.Error())
	}
	return loc
}
