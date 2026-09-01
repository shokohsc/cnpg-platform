package pg

import (
	"context"
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


