package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
}

type IndexInfo struct {
	Name    string `json:"name"`
	Primary bool   `json:"primary"`
	Unique  bool   `json:"unique"`
	Def     string `json:"def"`
}

type TableInfo struct {
	Name    string       `json:"name"`
	RowEst  int64        `json:"rowEst"`
	Columns []ColumnInfo `json:"columns"`
	Indexes []IndexInfo  `json:"indexes"`
}

type SchemaInfo struct {
	Name   string      `json:"name"`
	Tables []TableInfo `json:"tables"`
}

type TableResult struct {
	Columns []ColumnInfo `json:"columns"`
	Rows    [][]any      `json:"rows"`
	Total   int64        `json:"total"`
}

func (s *Server) ListTables(ctx context.Context, dbName string) ([]SchemaInfo, error) {
	conn, err := s.connForDB(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT n.nspname, c.relname, c.reltuples::bigint
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r','p') AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		ORDER BY n.nspname, c.relname`)
	if err != nil {
		return nil, normalizePGErr(err)
	}
	type tab struct {
		nsp, name string
		est       int64
	}
	var tabs []tab
	for rows.Next() {
		var t tab
		if err := rows.Scan(&t.nsp, &t.name, &t.est); err != nil {
			rows.Close()
			return nil, err
		}
		tabs = append(tabs, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	schemas := map[string]*SchemaInfo{}
	var order []string
	for _, t := range tabs {
		if _, ok := schemas[t.nsp]; !ok {
			schemas[t.nsp] = &SchemaInfo{Name: t.nsp}
			order = append(order, t.nsp)
		}
		schemas[t.nsp].Tables = append(schemas[t.nsp].Tables,
			TableInfo{Name: t.name, RowEst: t.est})
	}

	for _, sch := range schemas {
		for i := range sch.Tables {
			if err := loadTableMeta(ctx, conn, sch.Name, &sch.Tables[i]); err != nil {
				return nil, err
			}
		}
	}
	out := make([]SchemaInfo, 0, len(order))
	for _, n := range order {
		out = append(out, *schemas[n])
	}
	return out, nil
}

func loadTableMeta(ctx context.Context, conn *pgx.Conn, schema string, tbl *TableInfo) error {
	rel := QuoteIdent(schema) + "." + QuoteIdent(tbl.Name)
	cols, err := conn.Query(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '')
		FROM pg_attribute a
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE a.attrelid = $1::regclass AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, rel)
	if err != nil {
		return normalizePGErr(err)
	}
	defer cols.Close()
	for cols.Next() {
		var c ColumnInfo
		if err := cols.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default); err != nil {
			return err
		}
		tbl.Columns = append(tbl.Columns, c)
	}
	if err := cols.Err(); err != nil {
		return err
	}

	idex, err := conn.Query(ctx, `
		SELECT i.relname, ix.indisprimary, ix.indisunique, pg_get_indexdef(ix.indexrelid)
		FROM pg_index ix JOIN pg_class i ON i.oid = ix.indexrelid
		WHERE ix.indrelid = $1::regclass ORDER BY i.relname`, rel)
	if err != nil {
		return normalizePGErr(err)
	}
	defer idex.Close()
	for idex.Next() {
		var id IndexInfo
		if err := idex.Scan(&id.Name, &id.Primary, &id.Unique, &id.Def); err != nil {
			return err
		}
		tbl.Indexes = append(tbl.Indexes, id)
	}
	return idex.Err()
}

func (s *Server) ListRows(ctx context.Context, dbName, schema, table string, limit, offset int) (*TableResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	conn, err := s.connForDB(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rel := QuoteIdent(schema) + "." + QuoteIdent(table)
	def := "SELECT * FROM " + rel + fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := &TableResult{}
	rows, err := tx.Query(ctx, def)
	if err != nil {
		return nil, normalizePGErr(err)
	}
	defer rows.Close()

	cmeta, err := listColumns(ctx, conn, schema, table)
	if err != nil {
		return nil, err
	}
	out.Columns = cmeta
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make([]any, len(vals))
		for i, v := range vals {
			row[i] = ToJSON(v)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+rel).Scan(&out.Total); err != nil {
		return nil, normalizePGErr(err)
	}
	return out, tx.Commit(ctx)
}

func listColumns(ctx context.Context, conn *pgx.Conn, schema, table string) ([]ColumnInfo, error) {
	rel := QuoteIdent(schema) + "." + QuoteIdent(table)
	rows, err := conn.Query(ctx, `
		SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), '')
		FROM pg_attribute a
		LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
		WHERE a.attrelid = $1::regclass AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum`, rel)
	if err != nil {
		return nil, normalizePGErr(err)
	}
	defer rows.Close()
	var out []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
