package pg

import (
	"context"
	"fmt"
	"strings"
)

type RoleInfo struct {
	Name        string   `json:"name"`
	Super       bool     `json:"super"`
	Login       bool     `json:"login"`
	CreateDB    bool     `json:"createDB"`
	CreateRole  bool     `json:"createRole"`
	Replication bool     `json:"replication"`
	MemberOf    []string `json:"memberOf"`
	OwnedDBs    []string `json:"ownedDBs"`
}

type CreateRoleOptions struct {
	Super    bool
	CreateDB bool
	GrantDB  string
}

func createRoleSQL(name, password string, opts CreateRoleOptions) string {
	var b strings.Builder
	b.WriteString("CREATE ROLE " + QuoteIdent(name) + " LOGIN PASSWORD " + QuoteLit(password))
	if opts.Super {
		b.WriteString(" SUPERUSER")
	}
	if opts.CreateDB {
		b.WriteString(" CREATEDB")
	}
	return b.String()
}

func (s *Server) CreateRole(ctx context.Context, name, password string, opts CreateRoleOptions) error {
	if name == "" || len(name) > 63 {
		return fmt.Errorf("invalid role name")
	}
	_, err := s.conn.Exec(ctx, createRoleSQL(name, password, opts))
	if err != nil {
		return normalizePGErr(err)
	}
	if opts.GrantDB != "" {
		_, err = s.conn.Exec(ctx,
			"GRANT ALL PRIVILEGES ON DATABASE "+QuoteIdent(opts.GrantDB)+" TO "+QuoteIdent(name))
		if err != nil {
			return normalizePGErr(err)
		}
	}
	return nil
}

func (s *Server) ListRoles(ctx context.Context) ([]RoleInfo, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT r.rolname, r.rolsuper, r.rolcanlogin, r.rolcreatedb, r.rolcreaterole,
		       r.rolreplication,
		       COALESCE(array_agg(m.rolname) FILTER (WHERE m.rolname IS NOT NULL), '{}')
		FROM pg_roles r
		LEFT JOIN pg_auth_members am ON am.member = r.oid
		LEFT JOIN pg_roles m ON m.oid = am.roleid
		GROUP BY r.rolname, r.rolsuper, r.rolcanlogin, r.rolcreatedb, r.rolcreaterole, r.rolreplication ORDER BY r.rolname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RoleInfo
	for rows.Next() {
		var r RoleInfo
		if err := rows.Scan(&r.Name, &r.Super, &r.Login, &r.CreateDB, &r.CreateRole,
			&r.Replication, &r.MemberOf); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dbs, err := s.ListDatabases(ctx)
	if err != nil {
		return nil, err
	}
	byOwner := make(map[string][]string)
	for _, d := range dbs {
		byOwner[d.Owner] = append(byOwner[d.Owner], d.Name)
	}
	for i := range out {
		out[i].OwnedDBs = byOwner[out[i].Name]
	}
	return out, nil
}

func (s *Server) DropRole(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("invalid role name")
	}
	// ponytail: REASSIGN/DROP OWNED runs only in the current DB (postgres). Roles owning
	// objects in other DBs surface a PG error naming the objects; the UI shows it.
	var su string
	if err := s.conn.QueryRow(ctx, "SELECT rolname FROM pg_roles WHERE oid = 10").Scan(&su); err != nil {
		return normalizePGErr(err)
	}
	for _, stmt := range []string{
		"REASSIGN OWNED BY " + QuoteIdent(name) + " TO " + QuoteIdent(su),
		"DROP OWNED BY " + QuoteIdent(name),
		"DROP ROLE " + QuoteIdent(name),
	} {
		if _, err := s.conn.Exec(ctx, stmt); err != nil {
			return normalizePGErr(err)
		}
	}
	return nil
}

func (s *Server) RolePassword(ctx context.Context, name string) (string, error) {
	var pass string
	err := s.conn.QueryRow(ctx,
		"SELECT COALESCE(rolpassword, '') FROM pg_authid WHERE rolname = $1", name).Scan(&pass)
	if err != nil {
		return "", normalizePGErr(err)
	}
	return pass, nil
}
