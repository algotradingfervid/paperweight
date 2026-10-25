package gstin

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// pincodeRanges is STATE_PINCODE_MAPPING, india_compliance/gst_india/
// constants/__init__.py:197-234, verbatim but keyed by state code: the
// inclusive ranges of a pincode's first three digits. Other Territory (97)
// has none.
var pincodeRanges = map[string][][2]int{
	"01": {{180, 194}},                                                 // Jammu and Kashmir
	"02": {{171, 177}},                                                 // Himachal Pradesh
	"03": {{140, 160}},                                                 // Punjab
	"04": {{140, 140}, {160, 160}},                                     // Chandigarh
	"05": {{244, 263}},                                                 // Uttarakhand
	"06": {{121, 136}},                                                 // Haryana
	"07": {{110, 110}},                                                 // Delhi
	"08": {{301, 345}},                                                 // Rajasthan
	"09": {{201, 285}},                                                 // Uttar Pradesh
	"10": {{800, 855}},                                                 // Bihar
	"11": {{737, 737}},                                                 // Sikkim
	"12": {{790, 792}},                                                 // Arunachal Pradesh
	"13": {{797, 798}},                                                 // Nagaland
	"14": {{795, 795}},                                                 // Manipur
	"15": {{796, 796}},                                                 // Mizoram
	"16": {{799, 799}},                                                 // Tripura
	"17": {{793, 794}},                                                 // Meghalaya
	"18": {{781, 788}},                                                 // Assam
	"19": {{700, 743}},                                                 // West Bengal
	"20": {{813, 835}},                                                 // Jharkhand
	"21": {{751, 770}},                                                 // Odisha
	"22": {{490, 497}},                                                 // Chhattisgarh
	"23": {{450, 488}},                                                 // Madhya Pradesh
	"24": {{360, 396}},                                                 // Gujarat
	"26": {{362, 362}, {396, 396}},                                     // Dadra and Nagar Haveli and Daman and Diu
	"27": {{400, 445}},                                                 // Maharashtra
	"29": {{560, 591}},                                                 // Karnataka
	"30": {{403, 403}},                                                 // Goa
	"31": {{682, 682}},                                                 // Lakshadweep Islands
	"32": {{670, 695}},                                                 // Kerala
	"33": {{600, 643}},                                                 // Tamil Nadu
	"34": {{533, 533}, {605, 605}, {607, 607}, {609, 609}, {673, 673}}, // Puducherry
	"35": {{744, 744}},                                                 // Andaman and Nicobar Islands
	"37": {{500, 535}},                                                 // Andhra Pradesh
	"36": {{500, 509}, {518, 518}, {533, 533}},                         // Telangana
	"38": {{180, 180}, {181, 181}, {184, 184}, {190, 191}, {194, 194}}, // Ladakh
}

// ValidatePincode removes spaces from pin and checks PINCODE_FORMAT
// (utils/__init__.py:461, validate_pincode; constants/__init__.py:1474).
// A blank pincode is returned as "" with no error.
func ValidatePincode(pin string) (string, error) {
	p := strings.Join(strings.Fields(pin), "")
	if p != "" && !pincodePattern.MatchString(p) {
		return "", errors.New("Pincode must be 6 digits and can't start with 0.")
	}
	return p, nil
}

// PincodeInState checks a valid pincode's first three digits against the
// state's ranges (utils/__init__.py:480-493). States without ranges pass.
func PincodeInState(pin, stateCode string) error {
	ranges, ok := pincodeRanges[stateCode]
	if pin == "" || !ok {
		return nil
	}
	first3, err := strconv.Atoi(pin[:min(3, len(pin))])
	if err == nil {
		for _, r := range ranges {
			if r[0] <= first3 && first3 <= r[1] {
				return nil
			}
		}
	}
	s, _ := StateByCode(stateCode)
	return fmt.Errorf("Pincode %s is not in %s. Check the pincode or the state.", pin, s.Name)
}
