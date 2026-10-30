package mastersweb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// keepFor is how long an upload nobody imported survives (CD-201).
const keepFor = 24 * time.Hour

// store holds uploaded files in csv_upload between the Upload and Import
// steps (CD-201). A row is only ever reached with its owner's id.
type store struct {
	db    *db.DB
	clock clock.Clock
}

// put replaces u's upload with data, sweeps rows past keepFor and returns the
// new row's token.
func (s store) put(ctx context.Context, u auth.UserID, data []byte) (string, error) {
	now := s.clock.Now()
	token := rand.Text()
	err := s.db.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if _, err := q.ExecContext(ctx, `DELETE FROM csv_upload WHERE user_id = ? OR created_at < ?`,
			int64(u), auth.FormatTime(now.Add(-keepFor))); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `INSERT INTO csv_upload (token, user_id, created_at, data) VALUES (?, ?, ?, ?)`,
			token, int64(u), auth.FormatTime(now), data)
		return err
	})
	return token, err
}

// get returns u's file under token; ok is false when there is no such row,
// which is also what another user's token gets.
func (s store) get(ctx context.Context, token string, u auth.UserID) (data []byte, ok bool, err error) {
	err = s.db.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT data FROM csv_upload WHERE token = ? AND user_id = ?`, token, int64(u)).Scan(&data)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return data, err == nil, err
}

// claim takes u's row under token for one Import. It reports false when the
// row is gone or another Import holds it. A new upload replaces a claim that
// was never released, because put deletes by user.
func (s store) claim(ctx context.Context, token string, u auth.UserID) (bool, error) {
	var n int64
	err := s.db.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(ctx, `UPDATE csv_upload SET claimed_at = ? WHERE token = ? AND user_id = ? AND claimed_at IS NULL`,
			auth.FormatTime(s.clock.Now()), token, int64(u))
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n == 1, err
}

// release gives up a claim after an Import that imported nothing.
func (s store) release(ctx context.Context, token string, u auth.UserID) error {
	return s.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE csv_upload SET claimed_at = NULL WHERE token = ? AND user_id = ?`, token, int64(u))
		return err
	})
}

// remove deletes u's row under token.
func (s store) remove(ctx context.Context, token string, u auth.UserID) error {
	return s.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `DELETE FROM csv_upload WHERE token = ? AND user_id = ?`, token, int64(u))
		return err
	})
}
