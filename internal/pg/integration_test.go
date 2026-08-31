package pg

import (
	"os"
	"testing"
)

func testMeta(t *testing.T) (Meta, bool) {
	dsn := os.Getenv("CNPG_TEST_DSN")
	if dsn == "" {
		return Meta{}, false
	}
	return Meta{Name: "test", Namespace: "test", Host: "test", Port: 5432,
		Superuser: "postgres", Password: "", DSN: dsn}, true
}
