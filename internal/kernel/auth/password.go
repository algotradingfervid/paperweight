package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// HashParams are the argon2id parameters of a stored hash.
type HashParams struct {
	Memory, Time uint32
	Threads      uint8
	SaltLen      uint32
	KeyLen       uint32
}

// CurrentParams are the parameters new hashes use (plan Global Constraints).
var CurrentParams = HashParams{19456, 2, 1, 16, 32}

// DummyHash is a real hash of a throwaway password, verified against when an
// account doesn't exist so that sign-in takes the same time either way.
const DummyHash = "$argon2id$v=19$m=19456,t=2,p=1$YqqjLzOWCdaYdZKRSLAQNw$/hjJP8Fhvslp6OKwaCevhesZ6A+GuRBLReYIy1wwoUc"

var errBadHash = errors.New("auth: malformed password hash")

// NormalizePassword returns the NFC form of password. It never trims.
func NormalizePassword(password string) string { return norm.NFC.String(password) }

// Hash returns the PHC-encoded argon2id hash of the NFC-normalised password.
func Hash(password string) (string, error) {
	p := CurrentParams
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(NormalizePassword(password)), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// VerifyPassword checks password against encoded using the parameters stored
// in encoded. rehash is true when those parameters differ from CurrentParams.
// A malformed hash, or one whose parameters are outside the accepted bounds,
// is an error and never reaches argon2.
func VerifyPassword(encoded, password string) (ok, rehash bool, err error) {
	p, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(NormalizePassword(password)), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1, p != CurrentParams, nil
}

// parseHash strictly parses "$argon2id$v=19$m=<m>,t=<t>,p=<p>$<salt>$<key>"
// and bounds every parameter so a stored hash can't exhaust memory or CPU.
func parseHash(encoded string) (p HashParams, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return p, nil, nil, errBadHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return p, nil, nil, errBadHash
	}
	var vals [3]uint64
	for i, name := range []string{"m", "t", "p"} {
		v, ok := strings.CutPrefix(params[i], name+"=")
		if !ok {
			return p, nil, nil, errBadHash
		}
		if vals[i], err = strconv.ParseUint(v, 10, 32); err != nil {
			return p, nil, nil, errBadHash
		}
	}
	m, t, threads := vals[0], vals[1], vals[2]
	if threads < 1 || threads > 4 || t < 1 || t > 10 || m < 8*threads || m > 4*uint64(CurrentParams.Memory) {
		return p, nil, nil, errBadHash
	}
	enc := base64.RawStdEncoding
	if salt, err = enc.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return p, nil, nil, errBadHash
	}
	if key, err = enc.DecodeString(parts[5]); err != nil || len(key) < 16 || len(key) > 64 {
		return p, nil, nil, errBadHash
	}
	// All values were bounds-checked above, so the conversions can't overflow.
	p = HashParams{uint32(m), uint32(t), uint8(threads), uint32(len(salt)), uint32(len(key))} //nolint:gosec // bounded above
	return p, salt, key, nil
}
