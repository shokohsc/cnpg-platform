package pg

import (
	"strings"
	"testing"
)

func TestQuoteIdent(t *testing.T) {
	cases := map[string]string{
		"myapp":      `"myapp"`,
		`weird"name`: `"weird""name"`,
		"with space": `"with space"`,
		"MaKeD":      `"MaKeD"`,
		`a.b`:        `"a.b"`,
	}
	for in, want := range cases {
		if got := QuoteIdent(in); got != want {
			t.Errorf("QuoteIdent(%q)=%s want %s", in, got, want)
		}
	}
}

func TestQuoteLit(t *testing.T) {
	if got := QuoteLit(`O'Reilly`); got != `'O''Reilly'` {
		t.Errorf("got %s", got)
	}
	if got := QuoteLit(`x`); got != `'x'` {
		t.Errorf("got %s", got)
	}
}

func TestCreateSQL(t *testing.T) {
	if got := createDatabaseSQL("appdb", "", "template0", ""); got != `CREATE DATABASE "appdb" TEMPLATE "template0"` {
		t.Errorf("got %s", got)
	}
}

func TestCreateRoleSQL(t *testing.T) {
	if got := createRoleSQL("app", "s3cret", CreateRoleOptions{CreateDB: true, GrantDB: "appdb"});
		got != `CREATE ROLE "app" LOGIN PASSWORD 's3cret' CREATEDB` {
		t.Errorf("got %s", got)
	}
}

func TestSystemDatabase(t *testing.T) {
	for _, d := range []string{"postgres", "template0", "template1"} {
		if !systemDBs[d] {
			t.Errorf("%s should be system", d)
		}
	}
	if systemDBs["appdb"] {
		t.Error("appdb should not be system")
	}
}

func TestIsQuery(t *testing.T) {
	if !isQuery("  SELECT 1") || !isQuery("with x as (select 1) select * from x") ||
		!isQuery("SHOW work_mem") || !isQuery("values (1)") {
		t.Error("queries classified as non-query")
	}
	if isQuery("insert into t values (1)") || isQuery("  ;") || isQuery("") {
		t.Error("non-queries classified as query")
	}
}

func TestConnectURL(t *testing.T) {
	got := ConnectURL(URLParts{User: "app", Password: "p@ss", Host: "pg-rw.db.svc",
		Port: 5432, DB: "myapp", SSLMode: "require"})
	want := "postgresql://app:p%40ss@pg-rw.db.svc:5432/myapp?sslmode=require"
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}
	if !strings.Contains(ConnectURL(URLParts{User: "a", Password: "s", Host: "h", DB: "d", SSLMode: "verify-full"}), "sslmode=verify-full") {
		t.Error("missing sslmode")
	}
}

func TestToJSON(t *testing.T) {
	if ToJSON([]byte("hi")) != "hi" {
		t.Error("[]byte should decode to string")
	}
	if ToJSON(nil) != nil {
		t.Error("nil should stay nil")
	}
	if ToJSON(int64(5)) != int64(5) {
		t.Error("ints pass through")
	}
}
