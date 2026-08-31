package pg

import (
	"context"
	"os"
	"testing"
	"time"
)

func testMeta(t *testing.T) (Meta, bool) {
	dsn := os.Getenv("CNPG_TEST_DSN")
	if dsn == "" {
		return Meta{}, false
	}
	return Meta{Name: "test", Namespace: "test", Host: "test", Port: 5432,
		Superuser: "postgres", Password: "", DSN: dsn}, true
}

func TestIntegrationDatabases(t *testing.T) {
	m, ok := testMeta(t)
	if !ok {
		t.Skip("CNPG_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Connect(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	db := "itest_cnpg"
	_ = s.DropDatabase(ctx, db)
	if err := s.CreateDatabase(ctx, db, "", "template0", ""); err != nil {
		t.Fatal(err)
	}
	dbs, err := s.ListDatabases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range dbs {
		if d.Name == db {
			found = true
		}
	}
	if !found {
		t.Fatalf("database %s not listed", db)
	}
	if err := s.DropDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationRoles(t *testing.T) {
	m, ok := testMeta(t)
	if !ok {
		t.Skip("CNPG_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := Connect(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	role := "itest_role"
	_ = s.DropRole(ctx, role)
	if err := s.CreateRole(ctx, role, "pw123", CreateRoleOptions{}); err != nil {
		t.Fatal(err)
	}
	roles, err := s.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range roles {
		if r.Name == role {
			found = true
		}
	}
	if !found {
		t.Fatalf("role %s not listed", role)
	}
	if err := s.DropRole(ctx, role); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSQLAndTables(t *testing.T) {
	m, ok := testMeta(t)
	if !ok {
		t.Skip("CNPG_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := Connect(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	db := "itest_sql"
	_ = s.DropDatabase(ctx, db)
	if err := s.CreateDatabase(ctx, db, "", "template0", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecIn(ctx, db, `CREATE TABLE t1 (id bigserial primary key, name text)`); err != nil {
		t.Fatal(err)
	}
	res, err := s.RunSQL(ctx, db, `INSERT INTO t1 (name) VALUES ('a'), ('b')`, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.RowCount != 2 {
		t.Fatalf("expected 2 rows affected, got %d", res.RowCount)
	}
	q, err := s.RunSQL(ctx, db, `SELECT id, name FROM t1 ORDER BY id`, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Rows) != 2 || len(q.Columns) != 2 {
		t.Fatalf("bad query result %+v", q)
	}
	if _, err := s.RunSQL(ctx, db, `INSERT INTO t1 (name) VALUES ('c')`, true); err == nil {
		t.Fatal("readonly INSERT should fail")
	}
	tabs, err := s.ListTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) == 0 || len(tabs[0].Tables) == 0 {
		t.Fatal("no tables listed")
	}
	rows, err := s.ListRows(ctx, db, tabs[0].Name, tabs[0].Tables[0].Name, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Total != 2 {
		t.Fatalf("expected 2 total rows, got %d", rows.Total)
	}
	if err := s.DropDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
}
