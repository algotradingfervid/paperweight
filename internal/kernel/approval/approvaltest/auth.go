package approvaltest

import (
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/testkit"
)

// newAuth returns the shared auth fake (CD-56) carrying the fixture users,
// roles, rights matrix (the Administrator row with the wildcard DocType "*",
// CD-86) and restrictions, with team scope worked out through dir (CD-11).
func newAuth(fx testkit.Fixtures, dir auth.Directory) *authfake.Fake {
	f := authfake.New()
	f.UseDirectory(dir)
	byEmail := map[string]auth.UserID{}
	for _, u := range fx.Users {
		f.AddUser(auth.UserInfo{ID: u.ID, Email: u.Email, Name: u.Name, Enabled: !u.Disabled})
		for _, r := range u.Roles {
			f.AddRole(u.ID, r)
		}
		byEmail[u.Email] = u.ID
	}
	for _, row := range fx.Rights {
		if row.DocType == "every master" {
			continue // APR seeds no masters; WP2 maps this row onto its masters.
		}
		for _, r := range row.Rights {
			f.Grant(row.Role, row.DocType, auth.Right(r), auth.Scope(row.Scope))
		}
	}
	projects := map[string]int64{}
	for _, p := range fx.Projects {
		projects[p.Code] = p.ID
	}
	sites := map[string]int64{}
	for _, s := range fx.Sites {
		sites[s.Code] = s.ID
	}
	for _, r := range fx.Restrictions {
		f.Restrict(byEmail[r.Email], ids(r.ProjectCodes, projects), ids(r.SiteCodes, sites))
	}
	return f
}

// ids maps codes to database ids; nil (unrestricted) when there are no codes.
func ids(codes []string, byCode map[string]int64) []int64 {
	var out []int64
	for _, c := range codes {
		out = append(out, byCode[c])
	}
	return out
}
