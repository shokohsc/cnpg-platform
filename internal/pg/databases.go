package pg

import (
	"context"
)

type DBInfo struct {
	Name     string `json:"name"`
	Owner    string `json:"owner"`
	Encoding string `json:"encoding"`
	Template bool   `json:"template"`
	SizeKB   int64  `json:"sizeKB"`
	ACL      string `json:"acl,omitempty"`
}

func (s *Server) ListDatabases(ctx context.Context) ([]DBInfo, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT d.datname, pg_get_userbyid(d.datdba), pg_encoding_to_char(d.encoding),
		       d.datistemplate,
		       COALESCE(pg_database_size(d.datname)::bigint/1024, 0),
		       COALESCE(d.datacl::text, '')
		FROM pg_database d ORDER BY d.datname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DBInfo
	for rows.Next() {
		var d DBInfo
		if err := rows.Scan(&d.Name, &d.Owner, &d.Encoding, &d.Template, &d.SizeKB, &d.ACL); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
