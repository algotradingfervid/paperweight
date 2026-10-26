package wizard

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/alexedwards/scs/v2"
)

// SessionValues holds a Master-mode wizard's step values in the server-side
// session (CD-50), as one JSON string under Key so no gob registration is
// needed.
type SessionValues struct {
	SM  *scs.SessionManager
	Key string
}

// Get returns the values saved so far; never nil.
func (s SessionValues) Get(ctx context.Context) map[string]string {
	out := map[string]string{}
	if raw := s.SM.GetString(ctx, s.Key); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out) // written only by Merge
	}
	return out
}

// Merge adds step's values, overwriting keys already held.
func (s SessionValues) Merge(ctx context.Context, step map[string]string) {
	vals := s.Get(ctx)
	maps.Copy(vals, step)
	b, _ := json.Marshal(vals) // a map[string]string always marshals
	s.SM.Put(ctx, s.Key, string(b))
}

// Clear drops the wizard's values.
func (s SessionValues) Clear(ctx context.Context) { s.SM.Remove(ctx, s.Key) }
