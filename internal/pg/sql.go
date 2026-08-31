package pg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type SQLResult struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	Command  string   `json:"command"`
	RowCount int64    `json:"rowCount"`
}

func (s *Server) ExecIn(ctx context.Context, dbName, stmt string) (pgconn.CommandTag, error) {
	conn, err := s.connForDB(ctx, dbName)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer conn.Close(ctx)
	return conn.Exec(ctx, stmt)
}

func (s *Server) connForDB(ctx context.Context, dbName string) (*pgx.Conn, error) {
	cfg := s.conn.Config()
	cfg.Database = dbName
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, normalizePGErr(err)
	}
	return c, nil
}

func isQuery(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return false
	}
	for _, p := range []string{"select ", "with ", "show ", "explain ", "values ", "table "} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func runResult(ctx context.Context, q interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, stmt string) (*SQLResult, error) {
	if isQuery(stmt) {
		rows, err := q.Query(ctx, stmt)
		if err != nil {
			return nil, normalizePGErr(err)
		}
		defer rows.Close()
		res := &SQLResult{}
		if fd := rows.FieldDescriptions(); fd != nil {
			for _, f := range fd {
				res.Columns = append(res.Columns, string(f.Name))
			}
		}
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return nil, err
			}
			row := make([]any, len(vals))
			for i, v := range vals {
				row[i] = ToJSON(v)
			}
			res.Rows = append(res.Rows, row)
		}
		if err := rows.Err(); err != nil {
			return nil, normalizePGErr(err)
		}
		return res, nil
	}
	tag, err := q.Exec(ctx, stmt)
	if err != nil {
		return nil, normalizePGErr(err)
	}
	return &SQLResult{Command: tag.String(), RowCount: tag.RowsAffected()}, nil
}

func (s *Server) RunSQL(ctx context.Context, dbName, stmt string, readOnly bool) (*SQLResult, error) {
	conn, err := s.connForDB(ctx, dbName)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	if !readOnly {
		return runResult(ctx, conn, stmt)
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	res, err := runResult(ctx, tx, stmt)
	if err != nil {
		return nil, err
	}
	return res, tx.Commit(ctx)
}

func ToJSON(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		if s, ok := v.(fmt.Stringer); ok {
			return s.String()
		}
		return v
	}
}
