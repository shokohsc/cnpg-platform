package pg

import (
	"context"
	"fmt"
	"strings"
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

func createDatabaseSQL(name, owner, template, encoding string) string {
	var b strings.Builder
	b.WriteString("CREATE DATABASE " + QuoteIdent(name))
	if owner != "" {
		b.WriteString(" OWNER " + QuoteIdent(owner))
	}
	if template != "" {
		b.WriteString(" TEMPLATE " + QuoteIdent(template))
	}
	if encoding != "" {
		b.WriteString(" ENCODING " + QuoteLit(encoding))
	}
	return b.String()
}

func (s *Server) CreateDatabase(ctx context.Context, name, owner, template, encoding string) error {
	if name == "" || len(name) > 63 {
		return fmt.Errorf("invalid database name")
	}
	if isSystemDB(name) {
		return fmt.Errorf("%q is a system database", name)
	}
	_, err := s.conn.Exec(ctx, createDatabaseSQL(name, owner, template, encoding))
	if err != nil {
		return normalizePGErr(err)
	}
	return nil
}

func (s *Server) DropDatabase(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("invalid database name")
	}
	if isSystemDB(name) {
		return fmt.Errorf("%q is a system database", name)
	}
	_, err := s.conn.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname = `+QuoteLit(name)+` AND pid <> pg_backend_pid()`)
	if err != nil {
		return normalizePGErr(err)
	}
	_, err = s.conn.Exec(ctx, "DROP DATABASE "+QuoteIdent(name))
	return normalizePGErr(err)
}

func isSystemDB(name string) bool { return systemDBs[name] }
