package expensetest

import "testing"

// S4RevokeRead takes read on dt from every role of the user with email, and
// bumps AUTH's permission version so the change is seen.
func (e *Env) S4RevokeRead(t testing.TB, email, dt string) {
	t.Helper()
	uid := int64(e.P(t, email).ID)
	e.e8Exec(t, `DELETE FROM role_rights WHERE doctype = ? AND right_name = 'read'
	  AND role_id IN (SELECT role_id FROM user_roles WHERE user_id = ?)`, dt, uid)
	e.e8Exec(t, `UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`)
}

// S4Restrict limits the user with email to the project with code.
func (e *Env) S4Restrict(t testing.TB, email, code string) {
	t.Helper()
	e.e8Exec(t, `INSERT INTO user_restrictions (user_id, dimension, value_id) VALUES (?, 'project', ?)`,
		int64(e.P(t, email).ID), e.E8Project(t, code))
}
