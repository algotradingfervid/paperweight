package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// touchEvery is the most often a request moves a session's last_seen_at.
const touchEvery = time.Minute

// Session data keys.
const (
	SessionKeyUser = "uid" // int64 user id
	SessionKeyCSRF = "csrf"
)

// SessionConfig is the session policy (CD-14).
type SessionConfig struct {
	Secure         bool
	Idle, Absolute time.Duration
}

// DefaultSessionConfig is 12 h idle and 7 days absolute.
func DefaultSessionConfig(secure bool) SessionConfig {
	return SessionConfig{Secure: secure, Idle: 12 * time.Hour, Absolute: 7 * 24 * time.Hour}
}

// HashToken is base64url(SHA-256(raw)), the hash scs stores with HashTokenInStore.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type requestMetaKey struct{}

type requestMeta struct{ presentedHash, ip, userAgent string }

// WithRequestMeta returns ctx carrying the hash of the token the request
// presented and its client IP and user agent, which the store records.
func WithRequestMeta(ctx context.Context, presentedHash, ip, userAgent string) context.Context {
	return context.WithValue(ctx, requestMetaKey{}, requestMeta{presentedHash, ip, userAgent})
}

// PresentedHash returns the token hash set by WithRequestMeta.
func PresentedHash(ctx context.Context) string {
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	return m.presentedHash
}

// SessionStore is an scs.CtxStore over the sessions table. Expiry is decided
// here from the clock; scs's own expiry is ignored.
type SessionStore struct {
	db  *db.DB
	clk clock.Clock
	cfg SessionConfig
}

var (
	_ scs.CtxStore         = (*SessionStore)(nil)
	_ scs.IterableCtxStore = (*SessionStore)(nil)
)

// NewSessionStore returns a store over d's sessions table.
func NewSessionStore(d *db.DB, clk clock.Clock, cfg SessionConfig) *SessionStore {
	return &SessionStore{db: d, clk: clk, cfg: cfg}
}

// NewSessionManager builds the scs manager over st: hashed tokens and a
// browser-session cookie (CD-14). IdleTimeout must stay unset: with it scs marks
// every loaded session Modified, so each request rewrites the blob it loaded and
// an overlapping request's change is lost. The store decides idle expiry from
// last_seen_at, which Touch keeps current (CD-223).
func NewSessionManager(st *SessionStore, cfg SessionConfig) *scs.SessionManager {
	sm := scs.New()
	sm.Store = st
	sm.HashTokenInStore = true
	sm.Lifetime = cfg.Absolute
	name := "session"
	if cfg.Secure {
		name = "__Host-session"
	}
	sm.Cookie = scs.SessionCookie{
		Name:     name,
		HttpOnly: true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.Secure,
		Persist:  false,
	}
	return sm
}

// cutoffs returns the oldest last_seen_at and created_at still valid at now
// (both limits are inclusive).
func (st *SessionStore) cutoffs() (lastSeen, created string) {
	now := st.clk.Now()
	return FormatTime(now.Add(-st.cfg.Idle)), FormatTime(now.Add(-st.cfg.Absolute))
}

// FindCtx returns the data of a live session; an expired one is deleted.
func (st *SessionStore) FindCtx(ctx context.Context, hash string) ([]byte, bool, error) {
	var row dbq.GetSessionRow
	err := st.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		row, err = dbq.New(tx.SQL()).GetSession(ctx, hash)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	lastSeen, created := st.cutoffs()
	if row.LastSeenAt < lastSeen || row.CreatedAt < created {
		return nil, false, st.DeleteCtx(ctx, hash)
	}
	return row.Data, true, nil
}

// CommitCtx saves the session. The presented token is only updated, so a row
// revoked during the request stays gone; a new token is inserted.
func (st *SessionStore) CommitCtx(ctx context.Context, hash string, data []byte, _ time.Time) error {
	_, values, err := scs.GobCodec{}.Decode(data)
	if err != nil {
		return err
	}
	var uid any
	if v, present := values[SessionKeyUser]; present {
		id, ok := v.(int64)
		if !ok {
			return fmt.Errorf("auth: session %s is %T, want int64", SessionKeyUser, v)
		}
		uid = id
	}
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	now := FormatTime(st.clk.Now())
	return st.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		if hash == m.presentedHash {
			return q.UpdateSession(ctx, dbq.UpdateSessionParams{
				Data: data, UserID: uid, LastSeenAt: now, Ip: m.ip, UserAgent: m.userAgent, TokenHash: hash,
			})
		}
		return q.InsertSession(ctx, dbq.InsertSessionParams{
			TokenHash: hash, UserID: uid, Data: data, CreatedAt: now, LastSeenAt: now, Ip: m.ip, UserAgent: m.userAgent,
		})
	})
}

// Touch records that the session is in use: last_seen_at, ip and user_agent, at
// most once every touchEvery, and never data, so an unchanged session is not
// rewritten (CD-223). The cutoff is in the UPDATE's WHERE so overlapping requests
// write once. Matching no row (touched meanwhile, or revoked) is not an error.
func (st *SessionStore) Touch(ctx context.Context, hash string) error {
	now := st.clk.Now()
	cutoff := FormatTime(now.Add(-touchEvery))
	var lastSeen string
	err := st.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		lastSeen, err = dbq.New(tx.SQL()).SessionLastSeen(ctx, hash)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if lastSeen >= cutoff {
		return nil
	}
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	return st.db.Write(ctx, func(tx *db.Tx) error {
		return dbq.New(tx.SQL()).TouchSession(ctx, dbq.TouchSessionParams{
			Now: FormatTime(now), Ip: m.ip, UserAgent: m.userAgent, TokenHash: hash, Cutoff: cutoff,
		})
	})
}

// DeleteCtx deletes the session; a missing one is not an error.
func (st *SessionStore) DeleteCtx(ctx context.Context, hash string) error {
	return st.db.Write(ctx, func(tx *db.Tx) error {
		return dbq.New(tx.SQL()).DeleteSession(ctx, hash)
	})
}

// AllCtx returns the data of every live session by token hash.
func (st *SessionStore) AllCtx(ctx context.Context) (map[string][]byte, error) {
	lastSeen, created := st.cutoffs()
	out := map[string][]byte{}
	err := st.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListLiveSessions(ctx, dbq.ListLiveSessionsParams{LastSeenAt: lastSeen, CreatedAt: created})
		for _, r := range rows {
			out[r.TokenHash] = r.Data
		}
		return err
	})
	return out, err
}

// Find implements scs.Store.
func (st *SessionStore) Find(hash string) ([]byte, bool, error) {
	return st.FindCtx(context.Background(), hash)
}

// Commit implements scs.Store.
func (st *SessionStore) Commit(hash string, data []byte, expiry time.Time) error {
	return st.CommitCtx(context.Background(), hash, data, expiry)
}

// Delete implements scs.Store.
func (st *SessionStore) Delete(hash string) error {
	return st.DeleteCtx(context.Background(), hash)
}

// All implements scs.IterableStore.
func (st *SessionStore) All() (map[string][]byte, error) {
	return st.AllCtx(context.Background())
}

// SessionInfo describes one of a user's live sessions.
type SessionInfo struct {
	ID                    int64
	CreatedAt, LastSeenAt time.Time
	IP, UserAgent         string
	Current               bool // the session whose hash was passed as current
}

// ListForUser returns u's live sessions.
func (st *SessionStore) ListForUser(ctx context.Context, u UserID, currentHash string) ([]SessionInfo, error) {
	lastSeen, created := st.cutoffs()
	var rows []dbq.ListLiveSessionsForUserRow
	if err := st.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		rows, err = dbq.New(tx.SQL()).ListLiveSessionsForUser(ctx, dbq.ListLiveSessionsForUserParams{
			UserID: int64(u), LastSeenAt: lastSeen, CreatedAt: created,
		})
		return err
	}); err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(rows))
	for _, r := range rows {
		c, err := ParseTime(r.CreatedAt)
		if err != nil {
			return nil, err
		}
		l, err := ParseTime(r.LastSeenAt)
		if err != nil {
			return nil, err
		}
		out = append(out, SessionInfo{
			ID: r.ID, CreatedAt: c, LastSeenAt: l, IP: r.Ip, UserAgent: r.UserAgent, Current: r.TokenHash == currentHash,
		})
	}
	return out, nil
}

// RevokeForUser deletes u's session id; ErrNotFound if it is not u's.
func (st *SessionStore) RevokeForUser(ctx context.Context, u UserID, id int64) error {
	return st.db.Write(ctx, func(tx *db.Tx) error {
		n, err := dbq.New(tx.SQL()).DeleteSessionForUser(ctx, dbq.DeleteSessionForUserParams{ID: id, UserID: int64(u)})
		if err == nil && n == 0 {
			return ErrNotFound
		}
		return err
	})
}

// RevokeOthers deletes every session of u except the one with keepHash.
func (st *SessionStore) RevokeOthers(ctx context.Context, u UserID, keepHash string) error {
	return st.db.Write(ctx, func(tx *db.Tx) error { return st.RevokeAll(ctx, tx, u, keepHash) })
}

// RevokeAll deletes every session of u except the one with exceptHash ("" keeps none).
func (st *SessionStore) RevokeAll(ctx context.Context, tx *db.Tx, u UserID, exceptHash string) error {
	return dbq.New(tx.SQL()).DeleteSessionsForUser(ctx, dbq.DeleteSessionsForUserParams{UserID: int64(u), TokenHash: exceptHash})
}

// Sweep deletes expired sessions and returns how many it deleted.
func (st *SessionStore) Sweep(ctx context.Context) (int, error) {
	lastSeen, created := st.cutoffs()
	var n int64
	err := st.db.Write(ctx, func(tx *db.Tx) error {
		var err error
		n, err = dbq.New(tx.SQL()).DeleteExpiredSessions(ctx, dbq.DeleteExpiredSessionsParams{LastSeenAt: lastSeen, CreatedAt: created})
		return err
	})
	return int(n), err
}
