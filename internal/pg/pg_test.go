package pg

import "testing"

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
