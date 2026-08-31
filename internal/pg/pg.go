package pg

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Meta struct {
	Name      string
	Namespace string
	Host      string
	Port      int32
	Superuser string
	Password  string
	CA        []byte
	DSN       string // optional full override (tests / local dev)
}

type Server struct {
	conn *pgx.Conn
	meta Meta
}

func Connect(ctx context.Context, m Meta) (*Server, error) {
	dsn := m.DSN
	if dsn == "" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=require&application_name=cnpg-manager",
			url.QueryEscape(m.Superuser), url.QueryEscape(m.Password), m.Host, m.Port)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if len(m.CA) > 0 {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(m.CA) {
			cfg.TLSConfig = &tls.Config{RootCAs: pool, ServerName: strings.TrimSuffix(m.Host, ".svc")}
		}
	}
	// ponytail: if no CA was loaded we keep the DSN's sslmode=require (no cert
	// validation). Use verify-full once the operator-managed root CA is trusted.
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", m.Host, err)
	}
	return &Server{conn: conn, meta: m}, nil
}

func (s *Server) Close() { _ = s.conn.Close(context.Background()) }

func QuoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func QuoteLit(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

var systemDBs = map[string]bool{"postgres": true, "template0": true, "template1": true}
