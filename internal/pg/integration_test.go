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
