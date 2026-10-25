package types

import "encoding/json"

// MarshalJSON sends money as a decimal string, "1234.50", so no client
// reads it as a float (D-NUM-01).
func (m Money) MarshalJSON() ([]byte, error) { return json.Marshal(m.Plain()) }

// UnmarshalJSON accepts only a JSON string, read with ParseMoney. null is
// refused on purpose; optional amounts use *Money.
func (m *Money) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) == 0 || b[0] != '"' || json.Unmarshal(b, &s) != nil {
		return &InputError{Msg: `Amount must be sent as text, e.g. "1234.50".`}
	}
	v, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
