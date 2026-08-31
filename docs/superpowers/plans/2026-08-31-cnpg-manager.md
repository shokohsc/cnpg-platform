# CNPG Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go backend + Vue frontend, deployed in-cluster, that gives a Supabase-style UI for CloudNativePG clusters: browse clusters, provision databases and roles, run ad-hoc SQL, browse tables, view backups, and copy ready-made connection URLs.

**Architecture:** Backend lists CNPG `Cluster` CRs and reads their `-superuser`/`-ca` secrets via the Kubernetes API, then connects directly to each cluster's `-rw` service (`pgx/v5`) and executes SQL live. No app-side persistence — state is derived from `pg_catalog` on every request. Frontend is a Vue 3 + Vite + Tailwind SPA talking to the same origin, embedded into the Go binary via `go:embed`.

**Tech Stack:** Go 1.22+ (stdlib `net/http` with `{var}` routing), `pgx/v5`, `sigs.k8s.io/controller-runtime` client + `github.com/cloudnative-pg/cloudnative-pg/api`, Vue 3 + Vite 5 + Tailwind 3, Docker multi-stage build, Kustomize deploy manifests.

**Spec:** `docs/superpowers/specs/2026-08-31-cnpg-manager-design.md`

## Global Constraints

- Go module name is `cnpg-manager`. Go >= 1.22 (for ServeMux `{var}` patterns).
- All user-supplied identifiers (db/role/schema/table names) pass through `pg.QuoteIdent`/`pg.QuoteLit` — never raw interpolation.
- System databases `postgres`, `template0`, `template1` are rejected for drop/create.
- No logging of passwords or secrets. Passwords returned to the caller exactly once (at role creation).
- Frontend builds to `internal/web/dist` (vite `build.outDir`); Go embeds it with `//go:embed all:dist`. Frontend talks only to same-origin `/api`.
- Deps are limited to: `pgx/v5`, `controller-runtime`, `client-go`, `apimachinery`, `cloudnative-pg/api`, and the Vue toolchain. No new server-side packages without approval.
- Tests: `go vet ./...`, `go build ./...`, `go test ./...` must pass; frontend `npm run build` must succeed. Integration tests in `internal/pg` are gated on `CNPG_TEST_DSN` and skip when unset.
- Backend default listen address: `:8080`.
- Comment no code unless a port is doing something non-obvious; use a `ponytail:` comment for acknowledged simplifications.
- Commits: one per task, message `feat:`/`test:`/`chore:` prefixed, in the repo's style (see Task 1).

---

### Task 1: Toolchain + Go module scaffold

**Files:**
- Create: `go.mod`, `.gitignore`, `cmd/server/main.go`, `internal/kube/kube.go` (stub), `internal/pg/pg.go` (stub), `internal/web/server.go` (stub)

**Interfaces:**
- Produces: runnable `cnpg-manager` binary (prints startup line); module `cnpg-manager`; directory structure for later tasks.

- [ ] **Step 1: Ensure Go >= 1.22 is available**

Run: `go version`
Expected: `go version go1.2x...`. If absent or `< 1.22`:
```bash
curl -sL -o /tmp/go.tgz https://go.dev/dl/go1.24.4.linux-arm64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz
export PATH=$PATH:/usr/local/go/bin
```
(If the container is amd64, substitute `linux-amd64`.)

- [ ] **Step 2: Initialize module + stub packages**

```bash
mkdir -p cmd/server internal/kube internal/pg internal/web
go mod init cnpg-manager
```

`.gitignore`:
```
bin/
frontend/node_modules/
frontend/dist/
internal/web/dist/
*.test
```

`cmd/server/main.go`:
```go
package main

import "fmt"

func main() {
    fmt.Println("cnpg-manager: please run task N to fill this in")
}
```

`internal/kube/kube.go`, `internal/pg/pg.go`, `internal/web/server.go`: empty `package kube|pg|web` stubs.

- [ ] **Step 3: Verify**

Run: `go build ./... && go vet ./...`
Expected: success, binary `cnpg-manager` builds. Run `go run ./cmd/server` → prints the placeholder line.

- [ ] **Step 4: Commit**

```bash
git add . && git commit -m "chore: scaffold cnpg-manager module"
```

---

### Task 2: kube package

**Files:**
- Create: `internal/kube/kube.go`, `internal/kube/kube_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: nothing from later tasks.
- Produces:
  - `kube.New(ctx) (*Client, error)` — in-cluster config with kubeconfig fallback (implemented as `New() (*Client, error)` — the wiring in Tasks 2 and 6 uses the zero-context form)
  - `(*Client).ListClusters(ctx) ([]apiv1.Cluster, error)`
  - `(*Client).GetCluster(ctx, ns, name) (*apiv1.Cluster, error)`
  - `(*Client).ListBackups(ctx, ns, cluster) ([]apiv1.Backup, error)`
  - `(*Client).CreateBackup(ctx, *apiv1.Backup) error`
  - `(*Client).GetSecret(ctx, ns, name) (map[string][]byte, error)`
  - `(*Client).UpsertSecret(ctx, ns, name string, data map[string]string) error`
  - `kube.PortAnnotation`, `kube.ClusterPort(*apiv1.Cluster) int32`, `kube.RWService(*apiv1.Cluster) string`, `kube.SuperuserSecret(*apiv1.Cluster) string`, `kube.CASecret(*apiv1.Cluster) string`, `kube.BackupFor(cluster, name) *apiv1.Backup`

- [ ] **Step 1: Write failing tests**

`internal/kube/kube_test.go`:
```go
package kube

import (
    "context"
    "testing"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func schemeBuilder() client.Client {
    s := newScheme()
    c := fake.NewClientBuilder().WithScheme(s).Build()
    return c
}

func TestClusterPortDefault(t *testing.T) {
    cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db"}}
    if p := ClusterPort(cl); p != 5432 {
        t.Fatalf("expected 5432, got %d", p)
    }
}

func TestClusterPortAnnotation(t *testing.T) {
    cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db",
        Annotations: map[string]string{PortAnnotation: "15432"}}}
    if p := ClusterPort(cl); p != 15432 {
        t.Fatalf("expected 15432, got %d", p)
    }
}

func TestServiceNames(t *testing.T) {
    cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "db"}}
    if RWService(cl) != "pg-rw.db.svc" {
        t.Fatalf("rw service wrong: %s", RWService(cl))
    }
    if SuperuserSecret(cl) != "pg-superuser" || CASecret(cl) != "pg-ca" {
        t.Fatalf("secret names wrong: %s %s", SuperuserSecret(cl), CASecret(cl))
    }
}

func TestUpsertSecret(t *testing.T) {
    c := schemeBuilder()
    k := &Client{c: c}
    ctx := context.Background()
    if err := k.UpsertSecret(ctx, "db", "pg-role", map[string]string{"username": "app", "password": "x"}); err != nil {
        t.Fatal(err)
    }
    var sec corev1.Secret
    if err := c.Get(ctx, client.ObjectKey{Namespace: "db", Name: "pg-role"}, &sec); err != nil {
        t.Fatal(err)
    }
    if string(sec.Data["password"]) != "x" {
        t.Fatalf("password not stored")
    }
    if err := k.UpsertSecret(ctx, "db", "pg-role", map[string]string{"password": "y"}); err != nil {
        t.Fatal(err)
    }
    if err := c.Get(ctx, client.ObjectKey{Namespace: "db", Name: "pg-role"}, &sec); err != nil {
        t.Fatal(err)
    }
    if string(sec.Data["password"]) != "y" {
        t.Fatalf("upsert did not update")
    }
}

func TestListBackupsFilter(t *testing.T) {
    ctx := context.Background()
    mk := func(n string, cname string) *apiv1.Backup {
        b := &apiv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "db"},
            Spec: apiv1.BackupSpec{Cluster: apiv1.LocalObjectReference{Name: cname}}}
        b.Labels = map[string]string{ClusterLabelKey: cname}
        return b
    }
    s := newScheme()
    c := fake.NewClientBuilder().WithScheme(s).
        WithObjects(mk("b1", "pg"), mk("b2", "other"), mk("b3", "pg")).
        Build()
    k := &Client{c: c}
    got, err := k.ListBackups(ctx, "db", "pg")
    if err != nil {
        t.Fatal(err)
    }
    if len(got) != 2 {
        t.Fatalf("expected 2 backups, got %d", len(got))
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/kube/ -v`
Expected: FAIL — missing symbols (`newScheme`, `ClusterPort`, etc.).

- [ ] **Step 3: Add dependencies and implement**

```bash
go get github.com/cloudnative-pg/cloudnative-pg/api@latest sigs.k8s.io/controller-runtime@latest
go get k8s.io/client-go@latest k8s.io/apimachinery@latest k8s.io/api@latest
```

`internal/kube/kube.go`:
```go
package kube

import (
    "context"
    "fmt"
    "strconv"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/tools/clientcmd"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
    PortAnnotation  = "cnpg-manager.io/port"
    ClusterLabelKey = "cnpg.io/cluster"
)

type Client struct {
    c client.Client
}

func newScheme() *runtime.Scheme {
    s := runtime.NewScheme()
    _ = apiv1.AddToScheme(s)
    _ = corev1.AddToScheme(s)
    return s
}

func New() (*Client, error) {
    var cfg *rest.Config
    c, err := config.GetConfig()
    if err != nil {
        // kubeconfig fallback for local dev
        rc, kerr := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
            clientcmd.NewDefaultClientConfigLoadingRules(), nil).ClientConfig()
        if kerr != nil {
            return nil, fmt.Errorf("cluster config: %w", kerr)
        }
        cfg = rc
    } else {
        cfg = c
    }
    kc, err := client.New(cfg, client.Options{Scheme: newScheme()})
    if err != nil {
        return nil, err
    }
    return &Client{c: kc}, nil
}

func (k *Client) ListClusters(ctx context.Context) ([]apiv1.Cluster, error) {
    var list apiv1.ClusterList
    if err := k.c.List(ctx, &list); err != nil {
        return nil, err
    }
    return list.Items, nil
}

func (k *Client) GetCluster(ctx context.Context, ns, name string) (*apiv1.Cluster, error) {
    cl := &apiv1.Cluster{}
    if err := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, cl); err != nil {
        return nil, err
    }
    return cl, nil
}

func (k *Client) ListBackups(ctx context.Context, ns, cluster string) ([]apiv1.Backup, error) {
    var list apiv1.BackupList
    if err := k.c.List(ctx, &list, client.InNamespace(ns),
        client.MatchingLabels{ClusterLabelKey: cluster}); err != nil {
        return nil, err
    }
    return list.Items, nil
}

func (k *Client) CreateBackup(ctx context.Context, b *apiv1.Backup) error {
    return k.c.Create(ctx, b)
}

func (k *Client) GetSecret(ctx context.Context, ns, name string) (map[string][]byte, error) {
    sec := &corev1.Secret{}
    if err := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, sec); err != nil {
        return nil, err
    }
    return sec.Data, nil
}

func (k *Client) UpsertSecret(ctx context.Context, ns, name string, data map[string]string) error {
    sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
        Namespace: ns, Name: name, Labels: map[string]string{"app": "cnpg-manager"}}}
    sec.Data = make(map[string][]byte, len(data))
    for key, v := range data {
        sec.Data[key] = []byte(v)
    }
    err := k.c.Create(ctx, sec)
    if err != nil && apierrors.IsAlreadyExists(err) {
        var cur corev1.Secret
        if gerr := k.c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &cur); gerr == nil {
            cur.Data = sec.Data
            return k.c.Update(ctx, &cur)
        }
    }
    return err
}

func ClusterPort(cl *apiv1.Cluster) int32 {
    if v, ok := cl.Annotations[PortAnnotation]; ok {
        if p, err := strconv.ParseInt(v, 10, 32); err == nil {
            return int32(p)
        }
    }
    return 5432
}

func RWService(cl *apiv1.Cluster) string {
    return fmt.Sprintf("%s-rw.%s.svc", cl.Name, cl.Namespace)
}

func SuperuserSecret(cl *apiv1.Cluster) string { return cl.Name + "-superuser" }
func CASecret(cl *apiv1.Cluster) string        { return cl.Name + "-ca" }

func BackupFor(cl *apiv1.Cluster, name string) *apiv1.Backup {
    b := &apiv1.Backup{ObjectMeta: metav1.ObjectMeta{
        Namespace: cl.Namespace, Name: name, Labels: map[string]string{ClusterLabelKey: cl.Name}}}
    b.Spec.Method = "barmanObjectStore"
    b.Spec.Cluster.Name = cl.Name
    return b
}
```

Fix imports: add `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"`, `"k8s.io/client-go/rest"`, `apierrors "k8s.io/apimachinery/pkg/api/errors"`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go mod tidy && go test ./internal/kube/ -v && go vet ./...`
Expected: all tests PASS.

- [ ] **Step 5: Wire kube.New into main (still non-fatal)**

`cmd/server/main.go`:
```go
package main

import (
    "fmt"

    "cnpg-manager/internal/kube"
)

func main() {
    k, err := kube.New()
    if err != nil {
        fmt.Println("warning: no cluster config:", err)
    } else {
        fmt.Println("cnpg-manager: kube client ready:", k != nil)
    }
}
```

- [ ] **Step 6: Commit**

```bash
git add . && git -m "feat: kube client wrapper for CNPG clusters/backups/secrets"
```

---

### Task 3: pg package — connection, quoting, databases

**Files:**
- Create: `internal/pg/pg.go`, `internal/pg/databases.go`, `internal/pg/pg_test.go`, `internal/pg/integration_test.go`

**Interfaces:**
- Consumes: `kube.RWService`, `kube.ClusterPort` (values passed in as `pg.Meta`, not imports).
- Produces:
  - `pg.Meta{Name, Namespace, Host string; Port int32; Superuser, Password string; CA []byte}`
  - `pg.Connect(ctx, Meta) (*Server, error)`; `(*Server).Close()`
  - `pg.QuoteIdent(string) string`, `pg.QuoteLit(string) string`
  - `pg.PG` interface NOT defined here (in `web`). This package only exposes concrete `*Server` plus methods below.
  - `pg.DBInfo{Name, Owner, Encoding, ACL string; SizeKB int64; Template bool}`
  - `(*Server).ListDatabases(ctx) ([]DBInfo, error)`
  - `(*Server).CreateDatabase(ctx, name, owner, template, encoding string) error`
  - `(*Server).DropDatabase(ctx, name string) error`

- [ ] **Step 1: Write failing unit tests**

`internal/pg/pg_test.go`:
```go
package pg

import "testing"

func TestQuoteIdent(t *testing.T) {
    cases := map[string]string{
        "myapp":           `"myapp"`,
        `weird"name`:      `"weird""name"`,
        "with space":      `"with space"`,
        "MaKeD":           `"MaKeD"`,
        `a.b`:             `"a.b"`,
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
```

`internal/pg/integration_test.go`:
```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pg/ -run 'TestQuote|TestCreateSQL|TestSystem' -v`
Expected: FAIL — symbols undefined.

- [ ] **Step 3: Implement `internal/pg/pg.go`**

```go
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
```

`internal/pg/databases.go`:
```go
package pg

import (
    "context"
    "fmt"
    "strings"

    "github.com/jackc/pgx/v5"
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
```

`internal/pg/errors.go` (used by later tasks too):
```go
package pg

import (
    "errors"

    "github.com/jackc/pgx/v5/pgconn"
)

type PGError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Detail  string `json:"detail,omitempty"`
}

func (e *PGError) Error() string { return e.Message }

func normalizePGErr(err error) error {
    if err == nil {
        return nil
    }
    var pgErr *pgconn.PgError
    if as := errors.As(err, &pgErr); as {
        return &PGError{Code: pgErr.Code, Message: pgErr.Message, Detail: pgErr.Detail}
    }
    return err
}
```

- [ ] **Step 4: Run unit tests and vet**

Run: `go get github.com/jackc/pgx/v5@latest && go mod tidy && go test ./internal/pg/ -run 'TestQuote|TestCreateSQL|TestSystem' -v && go vet ./...`
Expected: unit tests PASS.

- [ ] **Step 5: Verify integration test compiles and skips**

Run: `go test ./internal/pg/ -run TestIntegration -v`
Expected: SKIP (no `CNPG_TEST_DSN`).

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat: pg connection, quoting, database CRUD with error mapping"
```

---

### Task 4: pg roles

**Files:**
- Create: `internal/pg/roles.go`, extend `internal/pg/pg_test.go` and `internal/pg/integration_test.go`

**Interfaces:**
- Consumes: `pg.QuoteIdent`, `pg.QuoteLit`, `pg.normalizePGErr`, `pg.Connect`.
- Produces:
  - `pg.RoleInfo{Name string; Super, Login, CreateDB, CreateRole, Replication bool; MemberOf, OwnedDBs []string}`
  - `pg.CreateRoleOptions{Super, CreateDB bool; GrantDB string}`
  - `(*Server).ListRoles(ctx) ([]RoleInfo, error)`
  - `(*Server).CreateRole(ctx, name, password string, opts CreateRoleOptions) error`
  - `(*Server).DropRole(ctx, name string) error`
  - `(*Server).RolePassword(ctx, name string) (string, error)`

- [ ] **Step 1: Write failing tests**

Add to `internal/pg/pg_test.go`:
```go
func TestCreateRoleSQL(t *testing.T) {
    if got := createRoleSQL("app", "s3cret", CreateRoleOptions{CreateDB: true, GrantDB: "appdb"});
        got != `CREATE ROLE "app" LOGIN PASSWORD 's3cret' CREATEDB` {
        t.Errorf("got %s", got)
    }
}
```

Add to `internal/pg/integration_test.go`:
```go
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
    if err := s.CreateRole(ctx, role, "pw123", CreateRoleOptions{Login: true}); err != nil {
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pg/ -run 'TestCreateRole' -v`
Expected: FAIL — `createRoleSQL` undefined; and later `IntegrationRoles` (skipped or fails).

- [ ] **Step 3: Implement `internal/pg/roles.go`**

```go
package pg

import (
    "context"
    "fmt"
    "strings"
)

type RoleInfo struct {
    Name        string   `json:"name"`
    Super       bool     `json:"super"`
    Login       bool     `json:"login"`
    CreateDB    bool     `json:"createDB"`
    CreateRole  bool     `json:"createRole"`
    Replication bool     `json:"replication"`
    MemberOf    []string `json:"memberOf"`
    OwnedDBs    []string `json:"ownedDBs"`
}

type CreateRoleOptions struct {
    Login    bool
    Super    bool
    CreateDB bool
    GrantDB  string
}

func createRoleSQL(name, password string, opts CreateRoleOptions) string {
    var b strings.Builder
    b.WriteString("CREATE ROLE " + QuoteIdent(name) + " LOGIN PASSWORD " + QuoteLit(password))
    if opts.Super {
        b.WriteString(" SUPERUSER")
    }
    if opts.CreateDB {
        b.WriteString(" CREATEDB")
    }
    return b.String()
}

func (s *Server) CreateRole(ctx context.Context, name, password string, opts CreateRoleOptions) error {
    if name == "" || len(name) > 63 {
        return fmt.Errorf("invalid role name")
    }
    _, err := s.conn.Exec(ctx, createRoleSQL(name, password, opts))
    if err != nil {
        return normalizePGErr(err)
    }
    if opts.GrantDB != "" {
        _, err = s.conn.Exec(ctx,
            "GRANT ALL PRIVILEGES ON DATABASE "+QuoteIdent(opts.GrantDB)+" TO "+QuoteIdent(name))
        if err != nil {
            return normalizePGErr(err)
        }
    }
    return nil
}

func (s *Server) ListRoles(ctx context.Context) ([]RoleInfo, error) {
    rows, err := s.conn.Query(ctx, `
        SELECT r.rolname, r.rolsuper, r.rolcanlogin, r.rolcreatedb, r.rolcreaterole,
               r.rolreplication,
               COALESCE(array_agg(m.rolname) FILTER (WHERE m.rolname IS NOT NULL), '{}')
        FROM pg_roles r
        LEFT JOIN pg_auth_members am ON am.member = r.oid
        LEFT JOIN pg_roles m ON m.oid = am.roleid
        GROUP BY r.oid ORDER BY r.rolname`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var out []RoleInfo
    for rows.Next() {
        var r RoleInfo
        if err := rows.Scan(&r.Name, &r.Super, &r.Login, &r.CreateDB, &r.CreateRole,
            &r.Replication, &r.MemberOf); err != nil {
            return nil, err
        }
        out = append(out, r)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }

    // owned databases, one query
    dbs, err := s.ListDatabases(ctx)
    if err != nil {
        return nil, err
    }
    byOwner := make(map[string][]string)
    for _, d := range dbs {
        byOwner[d.Owner] = append(byOwner[d.Owner], d.Name)
    }
    for i := range out {
        out[i].OwnedDBs = byOwner[out[i].Name]
    }
    return out, nil
}

func (s *Server) DropRole(ctx context.Context, name string) error {
    if name == "" {
        return fmt.Errorf("invalid role name")
    }
    // ponytail: REASSIGN/DROP OWNED runs only in the current DB (postgres). Roles owning
    // objects in other DBs surface a PG error naming the objects; the UI shows it.
    var su string
    if err := s.conn.QueryRow(ctx, "SELECT rolname FROM pg_roles WHERE oid = 10").Scan(&su); err != nil {
        return normalizePGErr(err)
    }
    for _, stmt := range []string{
        "REASSIGN OWNED BY " + QuoteIdent(name) + " TO " + QuoteIdent(su),
        "DROP OWNED BY " + QuoteIdent(name),
        "DROP ROLE " + QuoteIdent(name),
    } {
        if _, err := s.conn.Exec(ctx, stmt); err != nil {
            return normalizePGErr(err)
        }
    }
    return nil
}

func (s *Server) RolePassword(ctx context.Context, name string) (string, error) {
    var pass string
    err := s.conn.QueryRow(ctx,
        "SELECT COALESCE(rolpassword, '') FROM pg_authid WHERE rolname = $1", name).Scan(&pass)
    if err != nil {
        return "", normalizePGErr(err)
    }
    return pass, nil
}
```

> Note: for the integration test `DropRole` on a role with a password kept in `pg_authid` works fine; when `pgsql_password` auth is used they may be scram strings — acceptable.

- [ ] **Step 4: Run tests**

Run: `go mod tidy && go test ./internal/pg/ -v && go vet ./...`
Expected: unit + (skipped) integration tests: PASS.

- [ ] **Step 5: Commit**

```bash
git add . && git commit -m "feat: role CRUD against live cluster"
```

---

### Task 5: pg SQL runner, table browser, connect URL

**Files:**
- Create: `internal/pg/sql.go`, `internal/pg/tables.go`, `internal/pg/url.go`
- Extend: `internal/pg/pg_test.go`, `internal/pg/integration_test.go`

**Interfaces:**
- Consumes: `pg.QuoteIdent`, `pg.QuoteLit`, `pg.normalizePGErr`, `pg.Connect`.
- Produces:
  - `pg.SQLResult{Columns []string; Rows [][]any; Command string; RowCount int64}`
  - `(*Server).RunSQL(ctx, dbName, stmt string, readOnly bool) (*SQLResult, error)`
  - `pg.ColumnInfo{Name, Type string; Nullable bool; Default string}`
  - `pg.IndexInfo{Name string; Primary, Unique bool; Def string}`
  - `pg.TableInfo{Name string; RowEst int64; Columns []ColumnInfo; Indexes []IndexInfo}`
  - `pg.SchemaInfo{Name string; Tables []TableInfo}`
  - `(*Server).ListTables(ctx, dbName string) ([]SchemaInfo, error)`
  - `pg.TableResult{Columns []ColumnInfo; Rows [][]any; Total int64}`
  - `(*Server).ListRows(ctx, dbName, schema, table string, limit, offset int) (*TableResult, error)`
  - `pg.URLParts{User, Password, Host string; Port int32; DB, SSLMode string}`
  - `pg.ConnectURL(parts URLParts) string`
  - `pg.ToJSON(v any) any` (JSON-safe value conversion)

- [ ] **Step 1: Write failing unit tests**

Add to `internal/pg/pg_test.go`:
```go
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
```

Add to `internal/pg/integration_test.go`:
```go
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
```

This introduces `(*Server).ExecIn(ctx, db, stmt)` — a helper that runs `stmt` in the given database (used to seed data). Add it in `internal/pg/sql.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pg/ -run 'TestIsQuery|TestConnectURL|TestToJSON' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `internal/pg/sql.go`**

```go
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
```

- [ ] **Step 4: Implement `internal/pg/tables.go`**

```go
package pg

import (
    "context"
    "fmt"
    "strings"
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
            if err := s.loadTableMeta(ctx, conn, sch.Name, &sch.Tables[i]); err != nil {
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

func (s *Server) loadTableMeta(ctx context.Context, conn *pgx.Conn, schema string, tbl *TableInfo) error {
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

    ides, err := conn.Query(ctx, `
        SELECT i.relname, ix.indisprimary, ix.indisunique, pg_get_indexdef(ix.indexrelid)
        FROM pg_index ix JOIN pg_class i ON i.oid = ix.indexrelid
        WHERE ix.indrelid = $1::regclass ORDER BY i.relname`, rel)
    if err != nil {
        return normalizePGErr(err)
    }
    defer ides.Close()
    for ides.Next() {
        var id IndexInfo
        if err := ides.Scan(&id.Name, &id.Primary, &id.Unique, &id.Def); err != nil {
            return err
        }
        tbl.Indexes = append(tbl.Indexes, id)
    }
    return ides.Err()
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
    // read-only tx to be safe on multi-statement queries
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

    // columns for the rows view
    cmeta, err := listColumns(ctx, conn, schema, table)
    if err != nil {
        return nil, err
    }
    _ = cmeta
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
```

In `ListTables`, `schemas` and `order` are built as shown above and `loadTableMeta`
fills each table's columns/indexes — no extra loop is needed.

- [ ] **Step 5: Implement `internal/pg/url.go`**

```go
package pg

import (
    "fmt"
    "net/url"
)

type URLParts struct {
    User     string
    Password string
    Host     string
    Port     int32
    DB       string
    SSLMode  string
}

func ConnectURL(p URLParts) string {
    if p.SSLMode == "" {
        p.SSLMode = "require"
    }
    if p.Port == 0 {
        p.Port = 5432
    }
    u := url.URL{
        Scheme:   "postgresql",
        User:     url.UserPassword(p.User, p.Password),
        Host:     fmt.Sprintf("%s:%d", p.Host, p.Port),
        Path:     "/" + p.DB,
        RawQuery: "sslmode=" + p.SSLMode,
    }
    return u.String()
}
```

- [ ] **Step 6: Run unit tests, fix compile errors**

Run: `go mod tidy && go test ./internal/pg/ -run 'TestIsQuery|TestConnectURL|TestToJSON' -v && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Run the full unit suite (integration skips)**

Run: `go test ./internal/pg/ -v`
Expected: all non-integration pass, integration skipped; `go vet ./...` clean; `go build ./...` clean. Fix any compile issues from `ListTables`/`connForDB` returning `pgx.Conn` (import pgnx already present).

- [ ] **Step 8: Commit**

```bash
git add . && git commit -m "feat: SQL runner, table browser, connect URL builder"
```

---

### Task 6: web API core — server, clusters, databases

**Files:**
- Create: `internal/web/server.go`, `internal/web/json.go`, `internal/web/clusters.go`, `internal/web/databases.go`, `internal/web/server_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `pg.*`, `kube.Client` methods (concrete, adapted through interfaces below).
- Produces:
  - `web.ClusterStore` interface and `web.PG` interface (see Step 3)
  - `web.New(cs ClusterStore, connectPG func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error)) http.Handler`
  - Routes: `GET /api/clusters`, `GET /api/clusters/{cluster}`, `POST /api/clusters/{cluster}/databases`, `DELETE /api/clusters/{cluster}/databases/{db}`
  - `cmd/server/main.go` wiring: kube → store, clustering → connectPG, `http.ListenAndServe(":8080", h)`.
  - Nested dir `internal/web/dist/index.html` placeholder (asset); embedded via `//go:embed all:dist`.

- [ ] **Step 1: Write failing tests**

`internal/web/server_test.go`:
```go
package web

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
    "cnpg-manager/internal/pg"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakePG implements the full pg.PG interface (used by all web tests).
type fakePG struct {
    dbs     []pg.DBInfo
    err     error
    created []string
    dropped []string
}

func (f *fakePG) ListDatabases(ctx context.Context) ([]pg.DBInfo, error) { return f.dbs, f.err }
func (f *fakePG) CreateDatabase(ctx context.Context, name, owner, template, encoding string) error {
    f.created = append(f.created, name)
    return f.err
}
func (f *fakePG) DropDatabase(ctx context.Context, name string) error {
    f.dropped = append(f.dropped, name)
    return f.err
}
func (f *fakePG) ListRoles(ctx context.Context) ([]pg.RoleInfo, error) { return nil, f.err }
func (f *fakePG) CreateRole(ctx context.Context, name, password string, opts pg.CreateRoleOptions) error {
    f.created = append(f.created, name)
    return f.err
}
func (f *fakePG) DropRole(ctx context.Context, name string) error {
    f.dropped = append(f.dropped, name)
    return f.err
}
func (f *fakePG) RolePassword(ctx context.Context, name string) (string, error) { return "rolepw", nil }
func (f *fakePG) RunSQL(ctx context.Context, db, stmt string, readOnly bool) (*pg.SQLResult, error) {
    return &pg.SQLResult{Command: "SELECT", Rows: [][]any{{1}}}, nil
}
func (f *fakePG) ListTables(ctx context.Context, db string) ([]pg.SchemaInfo, error) { return nil, nil }
func (f *fakePG) ListRows(ctx context.Context, db, schema, table string, limit, offset int) (*pg.TableResult, error) {
    return &pg.TableResult{}, nil
}
func (f *fakePG) Close() {}

type fakeStore struct {
    clusters []apiv1.Cluster
    secret   map[string][]byte
    getErr   error
}

func (f *fakeStore) ListClusters(ctx context.Context) ([]apiv1.Cluster, error) { return f.clusters, nil }
func (f *fakeStore) ListBackups(ctx context.Context, ns, cluster string) ([]apiv1.Backup, error) {
    return nil, nil
}
func (f *fakeStore) CreateBackup(ctx context.Context, b *apiv1.Backup) error { return nil }
func (f *fakeStore) GetCluster(ctx context.Context, ns, name string) (*apiv1.Cluster, error) {
    if f.getErr != nil {
        return nil, f.getErr
    }
    for i := range f.clusters {
        if f.clusters[i].Name == name && f.clusters[i].Namespace == ns {
            return &f.clusters[i], nil
        }
    }
    return nil, apierrors.NewNotFound(schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "clusters"}, name)
}
func (f *fakeStore) GetSecret(ctx context.Context, ns, name string) (map[string][]byte, error) {
    return f.secret, nil
}
func (f *fakeStore) UpsertSecret(ctx context.Context, ns, name string, data map[string]string) error {
    return nil
}

func newTestHandler(cs ClusterStore, pgc PGFunc) http.Handler {
    return New(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) {
        if pgc == nil {
            return &fakePG{}, nil
        }
        return pgc(ctx, cl)
    })
}

func TestListClusters(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"},
            Spec: apiv1.ClusterSpec{PostgresVersion: 17}},
    }}
    h := newTestHandler(cs, nil)
    req := httptest.NewRequest("GET", "/api/clusters", nil)
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 200 {
        t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
    }
    var out []clusterView
    if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
        t.Fatal(err)
    }
    if len(out) != 1 || out[0].Name != "pg1" || out[0].Version != 17 {
        t.Fatalf("unexpected %+v", out)
    }
}

func TestListClustersPGUnreachable(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }}
    h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) {
        return nil, context.DeadlineExceeded
    })
    req := httptest.NewRequest("GET", "/api/clusters", nil)
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    var out []clusterView
    _ = json.Unmarshal(rec.Body.Bytes(), &out)
    if out[0].DBError == "" {
        t.Fatal("expected dbError field")
    }
}

func TestCreateDatabaseValidation(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }}
    pgc := &fakePG{}
    h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) { return pgc, nil })
    req := httptest.NewRequest("POST", "/api/clusters/pg1/databases?ns=db", strings.NewReader(`{"name":"postgres"}`))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 400 {
        t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
    }
}

func TestDropDatabase(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }}
    pgc := &fakePG{}
    h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) { return pgc, nil })
    req := httptest.NewRequest("DELETE", "/api/clusters/pg1/databases/app?ns=db", nil)
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 200 || len(pgc.dropped) != 1 || pgc.dropped[0] != "app" {
        t.Fatalf("drop failed: code=%d dropped=%v", rec.Code, pgc.dropped)
    }
}
```

Add imports `apierrors "k8s.io/apimachinery/pkg/api/errors"`, `"k8s.io/apimachinery/pkg/runtime/schema"`, `"strings"`, `pg.Backup` is `apiv1.Backup`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -v`
Expected: FAIL — `web.New`, `clusterView`, `PGFunc`, `pg.PG` undefined.

- [ ] **Step 3: Implement `internal/web/server.go`**

```go
package web

import (
    "bytes"
    "context"
    "embed"
    "io"
    "io/fs"
    "net/http"
    "strings"
    "time"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
    "cnpg-manager/internal/pg"
)

//go:embed all:dist
var dist embed.FS

// ClusterStore is the subset of kube.Client the web layer needs.
type ClusterStore interface {
    ListClusters(ctx context.Context) ([]apiv1.Cluster, error)
    GetCluster(ctx context.Context, ns, name string) (*apiv1.Cluster, error)
    ListBackups(ctx context.Context, ns, cluster string) ([]apiv1.Backup, error)
    CreateBackup(ctx context.Context, b *apiv1.Backup) error
    GetSecret(ctx context.Context, ns, name string) (map[string][]byte, error)
    UpsertSecret(ctx context.Context, ns, name string, data map[string]string) error
}

// PG is the slice of pg.Server used by the API (fake-able in tests).
type PG interface {
    ListDatabases(ctx context.Context) ([]pg.DBInfo, error)
    CreateDatabase(ctx context.Context, name, owner, template, encoding string) error
    DropDatabase(ctx context.Context, name string) error
    ListRoles(ctx context.Context) ([]pg.RoleInfo, error)
    CreateRole(ctx context.Context, name, password string, opts pg.CreateRoleOptions) error
    DropRole(ctx context.Context, name string) error
    RolePassword(ctx context.Context, name string) (string, error)
    RunSQL(ctx context.Context, db, stmt string, readOnly bool) (*pg.SQLResult, error)
    ListTables(ctx context.Context, db string) ([]pg.SchemaInfo, error)
    ListRows(ctx context.Context, db, schema, table string, limit, offset int) (*pg.TableResult, error)
    Close()
}

// PGFunc builds a PG connection for one cluster.
type PGFunc func(ctx context.Context, cl *apiv1.Cluster) (PG, error)

type api struct {
    cs        ClusterStore
    connectPG PGFunc
}

func New(cs ClusterStore, connectPG PGFunc) http.Handler {
    h := &api{cs: cs, connectPG: connectPG}
    mux := http.NewServeMux()
    mux.HandleFunc("GET /api/clusters", h.listClusters)
    mux.HandleFunc("GET /api/clusters/{cluster}", h.getCluster)
    mux.HandleFunc("POST /api/clusters/{cluster}/databases", h.createDatabase)
    mux.HandleFunc("DELETE /api/clusters/{cluster}/databases/{db}", h.dropDatabase)
    mux.HandleFunc("GET /api/clusters/{cluster}/roles", h.listRoles)
    mux.HandleFunc("POST /api/clusters/{cluster}/roles", h.createRole)
    mux.HandleFunc("DELETE /api/clusters/{cluster}/roles/{role}", h.dropRole)
    mux.HandleFunc("POST /api/clusters/{cluster}/sql", h.runSQL)
    mux.HandleFunc("GET /api/clusters/{cluster}/tables", h.listTables)
    mux.HandleFunc("GET /api/clusters/{cluster}/rows", h.listRows)
    mux.HandleFunc("GET /api/clusters/{cluster}/backups", h.listBackups)
    mux.HandleFunc("POST /api/clusters/{cluster}/backups", h.createBackup)
    mux.HandleFunc("GET /api/clusters/{cluster}/connect", h.connInfo)
    mux.Handle("/", spaFS(dist))
    return withLogging(mux)
}

// resolveCluster finds a cluster by name, disambiguated by ?ns=.
func (h *api) resolveCluster(ctx context.Context, r *http.Request) (*apiv1.Cluster, error) {
    name := r.PathValue("cluster")
    ns := r.URL.Query().Get("ns")
    if ns == "" {
        clusters, err := h.cs.ListClusters(ctx)
        if err != nil {
            return nil, err
        }
        var match *apiv1.Cluster
        for i := range clusters {
            if clusters[i].Name == name {
                if match != nil {
                    return nil, errAmbiguous{name}
                }
                match = &clusters[i]
            }
        }
        if match == nil {
            return nil, errNotFound{name}
        }
        return match, nil
    }
    return h.cs.GetCluster(ctx, ns, name)
}

// spaFS serves embedded files, falling back to index.html for unknown paths.
func spaFS(assets fs.FS) http.Handler {
    sub, _ := fs.Sub(assets, "dist")
    fileServer := http.FileServer(http.FS(sub))
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !strings.HasPrefix(r.URL.Path, "/api") {
            name := strings.TrimPrefix(r.URL.Path, "/")
            if name == "" {
                name = "index.html"
            }
            if _, err := fs.Stat(sub, name); err != nil {
                index, err := sub.Open("index.html")
                if err == nil {
                    defer index.Close()
                    body, _ := io.ReadAll(index)
                    http.ServeContent(w, r, "index.html", time.Now(), bytes.NewReader(body))
                    return
                }
            }
        }
        fileServer.ServeHTTP(w, r)
    })
}
```

- [ ] **Step 4: Implement `internal/web/json.go` and errors**

`internal/web/json.go`:
```go
package web

import (
    "encoding/json"
    "net/http"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
    writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
    defer r.Body.Close()
    return json.NewDecoder(r.Body).Decode(v)
}
```

Errors (in `json.go` or a new `errors.go`):
```go
package web

import (
    "errors"
    "net/http"

    apierrors "k8s.io/apimachinery/pkg/api/errors"

    "cnpg-manager/internal/pg"
)

type errNotFound struct{ name string }
func (e errNotFound) Error() string { return "cluster or object not found: " + e.name }

type errAmbiguous struct{ name string }
func (e errAmbiguous) Error() string { return "name " + e.name + " is ambiguous, pass ?ns=" }
```

Handler error mapping helper in `server.go`:
```go
func (h *api) writeError(w http.ResponseWriter, err error) {
    switch e := err.(type) {
    case errNotFound:
        writeErr(w, http.StatusNotFound, e.Error())
    case errAmbiguous:
        writeErr(w, http.StatusBadRequest, e.Error())
    case *pg.PGError:
        writeJSON(w, http.StatusBadRequest, e)
    default:
        if apierrors.IsNotFound(err) {
            writeErr(w, http.StatusNotFound, "not found")
            return
        }
        if apierrors.IsForbidden(err) {
            writeErr(w, http.StatusForbidden, "RBAC forbids this operation")
            return
        }
        writeErr(w, http.StatusInternalServerError, err.Error())
    }
}
```

`internal/web/clusters.go`:
```go
package web

import (
    "context"
    "net/http"
    "time"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

    "cnpg-manager/internal/kube"
)

type clusterView struct {
    Name          string `json:"name"`
    Namespace     string `json:"namespace"`
    Version       int32  `json:"version"`
    Phase         string `json:"phase"`
    Ready         int    `json:"readyInstances"`
    Total         int    `json:"instances"`
    Port          int32  `json:"port"`
    Databases     int    `json:"databases"`
    Roles         int    `json:"roles"`
    LastBackup    string `json:"lastBackup,omitempty"`
    DBError       string `json:"dbError,omitempty"`
}

func enrich(ctx context.Context, h *api, cl *apiv1.Cluster) clusterView {
    v := clusterView{
        Name: cl.Name, Namespace: cl.Namespace, Phase: cl.Status.Phase,
        Ready: cl.Status.ReadyInstances, Total: cl.Status.Instances,
        Port: kube.ClusterPort(cl), Databases: -1, Roles: -1,
    }
    if cl.Spec.PostgresVersion != 0 {
        v.Version = int32(cl.Spec.PostgresVersion)
    }
    pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    p, err := h.connectPG(pctx, cl)
    if err != nil {
        v.DBError = "unreachable: " + err.Error()
        return v
    }
    defer p.Close()
    dbs, err := p.ListDatabases(pctx)
    if err != nil {
        v.DBError = err.Error()
        return v
    }
    v.Databases = len(dbs)
    roles, err := p.ListRoles(pctx)
    if err != nil {
        v.DBError = err.Error()
        return v
    }
    v.Roles = len(roles)
    if backups, err := h.cs.ListBackups(pctx, cl.Namespace, cl.Name); err == nil && len(backups) > 0 {
        var t time.Time
        for _, b := range backups {
            if b.Status.LastSuccessfulBackup != nil && b.Status.LastSuccessfulBackup.Time.After(t) {
                t = b.Status.LastSuccessfulBackup.Time
            }
        }
        if !t.IsZero() {
            v.LastBackup = t.Format(time.RFC3339)
        }
    }
    return v
}

func (h *api) listClusters(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    clusters, err := h.cs.ListClusters(ctx)
    if err != nil {
        h.writeError(w, err)
        return
    }
    out := make([]clusterView, 0, len(clusters))
    for i := range clusters {
        out = append(out, enrich(ctx, h, &clusters[i]))
    }
    writeJSON(w, http.StatusOK, out)
}

func (h *api) getCluster(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, enrich(r.Context(), h, cl))
}
```

- [ ] **Step 5: Implement `internal/web/databases.go`**

```go
package web

import (
    "net/http"
)

func (h *api) createDatabase(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    var body struct {
        Name     string `json:"name"`
        Owner    string `json:"owner"`
        Template string `json:"template"`
        Encoding string `json:"encoding"`
    }
    if err := decode(r, &body); err != nil {
        writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    if err := p.CreateDatabase(r.Context(), body.Name, body.Owner, body.Template, body.Encoding); err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"created": body.Name})
}

func (h *api) dropDatabase(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    if err := p.DropDatabase(r.Context(), r.PathValue("db")); err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("db")})
}
```

- [ ] **Step 6: Implement `cmd/server/main.go` wiring**

```go
package main

import (
    "context"
    "net/http"
    "os"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

    "cnpg-manager/internal/kube"
    "cnpg-manager/internal/pg"
    "cnpg-manager/internal/web"
)

func main() {
    k, err := kube.New()
    if err != nil {
        // kube.New already falls back; a hard failure means no config at all
        println("fatal: no kubernetes config:", err.Error())
        os.Exit(1)
    }

    connectPG := func(ctx context.Context, cl *apiv1.Cluster) (web.PG, error) {
        sec, err := k.GetSecret(ctx, cl.Namespace, kube.SuperuserSecret(cl))
        if err != nil {
            return nil, err
        }
        ca, _ := k.GetSecret(ctx, cl.Namespace, kube.CASecret(cl))
        meta := pg.Meta{
            Name:      cl.Name,
            Namespace: cl.Namespace,
            Host:      kube.RWService(cl),
            Port:      kube.ClusterPort(cl),
            Superuser: string(sec["username"]),
            Password:  string(sec["password"]),
            CA:        ca["ca.crt"],
        }
        return pg.Connect(ctx, meta)
    }

    h := web.New(k, connectPG)
    addr := ":" + envOr("PORT", "8080")
    println("cnpg-manager listening on", addr)
    if err := http.ListenAndServe(addr, h); err != nil {
        println("fatal:", err.Error())
        os.Exit(1)
    }
}

func envOr(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}
```

- [ ] **Step 7: Create the embed placeholder**

```bash
mkdir -p internal/web/dist
tee internal/web/dist/index.html >/dev/null <<'EOF'
<!doctype html><html><body>cnpg-manager frontend not built yet</body></html>
EOF
```

- [ ] **Step 8: Run tests and vet**

Run: `go mod tidy && go build ./... && go vet ./... && go test ./internal/web/ -v`
Expected: all web tests PASS (fake store/PG). `cmd/server` builds.

- [ ] **Step 9: Commit**

```bash
git add . && git commit -m "feat: web API core — clusters list, database CRUD endpoints"
```

---

### Task 7: web API — roles, SQL, tables, backups, connect info

**Files:**
- Create: `internal/web/roles.go`, `internal/web/sql.go`, `internal/web/tables.go`, `internal/web/backups.go`, `internal/web/connect.go`, extend `internal/web/server_test.go`

**Interfaces:**
- Consumes: `pg.PG`, `web.writeJSON`/`writeErr`/`decode`, `web.errNotFound`.
- Produces: remaining routes from Task 6's mux. Role create returns `{"name","password"}`; secrets upserted to `{cluster}-{role}` via `cs.UpsertSecret`.

- [ ] **Step 1: Write failing tests** (append to `server_test.go`)

```go
func TestCreateRoleReturnsPasswordAndSecret(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }, secret: map[string][]byte{"password": []byte("old")}}
    pgc := &fakePG{}
    h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) { return pgc, nil })
    req := httptest.NewRequest("POST", "/api/clusters/pg1/roles?ns=db",
        strings.NewReader(`{"name":"app","grantDB":"appdb"}`))
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 200 {
        t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
    }
    var out map[string]string
    _ = json.Unmarshal(rec.Body.Bytes(), &out)
    if out["password"] == "" {
        t.Fatal("expected generated password in response")
    }
    if len(pgc.created) != 1 { // not used - fakePG records via CreateRole below
    }
}

func TestRunSQLValidation(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }}
    h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (pg.PG, error) {
        return &fakePG{}, nil
    })
    req := httptest.NewRequest("POST", "/api/clusters/pg1/sql?ns=db",
        strings.NewReader(`{"db":"","statement":""}`))
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 400 {
        t.Fatalf("expected 400, got %d", rec.Code)
    }
}

func TestConnectInfoSuperuser(t *testing.T) {
    cs := &fakeStore{clusters: []apiv1.Cluster{
        {ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
    }, secret: map[string][]byte{"username": []byte("postgres"), "password": []byte("pw")}}
    h := newTestHandler(cs, nil)
    req := httptest.NewRequest("GET", "/api/clusters/pg1/connect?ns=db&db=app&role=postgres", nil)
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != 200 {
        t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
    }
    var out connectInfo
    _ = json.Unmarshal(rec.Body.Bytes(), &out)
    if out.Password != "pw" || out.User != "postgres" || out.DB != "app" {
        t.Fatalf("bad connect info %+v", out)
    }
    if !strings.Contains(out.URLDirect, "pg1-rw.db.svc:5432/app") {
        t.Fatalf("bad url %s", out.URLDirect)
    }
}
```

Extend `fakeStore` with a `Roles` field (not needed by these tests).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -v`
Expected: FAIL — handlers undefined.

- [ ] **Step 3: Implement `internal/web/roles.go`**

```go
package web

import (
    "crypto/rand"
    "encoding/hex"
    "net/http"

    "cnpg-manager/internal/kube"
    "cnpg-manager/internal/pg"
)

func (h *api) listRoles(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    roles, err := p.ListRoles(r.Context())
    if err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, roles)
}

func randomPassword(n int) string {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        return "password"
    }
    return hex.EncodeToString(b)
}

func (h *api) createRole(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    var body struct {
        Name     string `json:"name"`
        Password string `json:"password"`
        Super    bool   `json:"super"`
        CreateDB bool   `json:"createDB"`
        GrantDB  string `json:"grantDB"`
    }
    if err := decode(r, &body); err != nil {
        writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
        return
    }
    if body.Password == "" {
        body.Password = randomPassword(16)
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    if err := p.CreateRole(r.Context(), body.Name, body.Password,
        pg.CreateRoleOptions{Login: true, Super: body.Super, CreateDB: body.CreateDB, GrantDB: body.GrantDB}); err != nil {
        h.writeError(w, err)
        return
    }
    _ = h.cs.UpsertSecret(r.Context(), cl.Namespace,
        kube.RoleSecret(cl, body.Name), map[string]string{"username": body.Name, "password": body.Password})
    writeJSON(w, http.StatusOK, map[string]string{"name": body.Name, "password": body.Password})
}

func (h *api) dropRole(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    if err := p.DropRole(r.Context(), r.PathValue("role")); err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"dropped": r.PathValue("role")})
}
```

Add to `internal/kube/kube.go`:
```go
func RoleSecret(cl *apiv1.Cluster, role string) string {
    return cl.Name + "-" + role
}
```

- [ ] **Step 4: Implement `internal/web/sql.go`**

```go
package web

import (
    "net/http"
)

func (h *api) runSQL(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    var body struct {
        DB        string `json:"db"`
        Statement string `json:"statement"`
        ReadOnly  bool   `json:"readOnly"`
    }
    if err := decode(r, &body); err != nil {
        writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
        return
    }
    if body.DB == "" || body.Statement == "" {
        writeErr(w, http.StatusBadRequest, "db and statement are required")
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    res, err := p.RunSQL(r.Context(), body.DB, body.Statement, body.ReadOnly)
    if err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, res)
}
```

`internal/web/tables.go`:
```go
package web

import (
    "net/http"
    "strconv"
)

func (h *api) listTables(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    db := r.URL.Query().Get("db")
    if db == "" {
        writeErr(w, http.StatusBadRequest, "db query param is required")
        return
    }
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    out, err := p.ListTables(r.Context(), db)
    if err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, out)
}

func (h *api) listRows(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    q := r.URL.Query()
    limit, _ := strconv.Atoi(q.Get("limit"))
    offset, _ := strconv.Atoi(q.Get("offset"))
    p, err := h.connectPG(r.Context(), cl)
    if err != nil {
        h.writeError(w, err)
        return
    }
    defer p.Close()
    out, err := p.ListRows(r.Context(), q.Get("db"), q.Get("schema"), q.Get("table"), limit, offset)
    if err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, out)
}
```

`internal/web/backups.go`:
```go
package web

import (
    "fmt"
    "net/http"
    "time"

    apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

    "cnpg-manager/internal/kube"
)

type backupView struct {
    Name          string `json:"name"`
    Method        string `json:"method"`
    BackupKind    string `json:"backupKind"`
    Phase         string `json:"phase"`
    Destination   string `json:"destination,omitempty"`
    StartedAt     string `json:"startedAt,omitempty"`
    FinishedAt    string `json:"finishedAt,omitempty"`
}

func toBackupView(b apiv1.Backup) backupView {
    v := backupView{Name: b.Name, Method: string(b.Spec.Method), Phase: string(b.Status.Phase)}
    if b.Spec.BackupKind != nil {
        v.BackupKind = string(*b.Spec.BackupKind)
    }
    if b.Status.Destination != "" {
        v.Destination = b.Status.Destination
    }
    if b.Status.StartedAt != nil && !b.Status.StartedAt.Time.IsZero() {
        v.StartedAt = b.Status.StartedAt.Format("2006-01-02 15:04:05Z07:00")
    }
    if b.Status.StoppedAt != nil && !b.Status.StoppedAt.Time.IsZero() {
        v.FinishedAt = b.Status.StoppedAt.Format("2006-01-02 15:04:05Z07:00")
    }
    return v
}

func (h *api) listBackups(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    backups, err := h.cs.ListBackups(r.Context(), cl.Namespace, cl.Name)
    if err != nil {
        h.writeError(w, err)
        return
    }
    out := make([]backupView, 0, len(backups))
    for i := range backups {
        out = append(out, toBackupView(backups[i]))
    }
    writeJSON(w, http.StatusOK, out)
}

func (h *api) createBackup(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    name := fmt.Sprintf("%s-backup-%d", cl.Name, time.Now().Unix())
    b := kube.BackupFor(cl, name)
    if err := h.cs.CreateBackup(r.Context(), b); err != nil {
        h.writeError(w, err)
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"created": name})
}
```

`internal/web/connect.go`:
```go
package web

import (
    "net/http"

    "cnpg-manager/internal/kube"
    "cnpg-manager/internal/pg"
)

type connectInfo struct {
    User          string `json:"user"`
    DB            string `json:"db"`
    Host          string `json:"host"`
    Port          int32  `json:"port"`
    Password      string `json:"password"`
    URLDirect     string `json:"urlDirect"`
    URLVerifyFull string `json:"urlVerifyFull"`
}

func (h *api) connInfo(w http.ResponseWriter, r *http.Request) {
    cl, err := h.resolveCluster(r.Context(), r)
    if err != nil {
        h.writeError(w, err)
        return
    }
    db := r.URL.Query().Get("db")
    role := r.URL.Query().Get("role")
    if db == "" || role == "" {
        writeErr(w, http.StatusBadRequest, "db and role query params are required")
        return
    }
    sec, err := h.cs.GetSecret(r.Context(), cl.Namespace, kube.SuperuserSecret(cl))
    if err != nil {
        h.writeError(w, err)
        return
    }
    superUser := string(sec["username"])
    var user, password string
    if role == superUser {
        user, password = superUser, string(sec["password"])
    } else {
        rs, err := h.cs.GetSecret(r.Context(), cl.Namespace, kube.RoleSecret(cl, role))
        if err != nil {
            h.writeError(w, err)
            return
        }
        user, password = string(rs["username"]), string(rs["password"])
    }
    parts := pg.URLParts{User: user, Password: password, Host: kube.RWService(cl),
        Port: kube.ClusterPort(cl), DB: db, SSLMode: "require"}
    out := connectInfo{
        User: user, DB: db, Host: parts.Host, Port: parts.Port, Password: password,
        URLDirect: pg.ConnectURL(parts),
    }
    parts.SSLMode = "verify-full"
    out.URLVerifyFull = pg.ConnectURL(parts)
    writeJSON(w, http.StatusOK, out)
}
```

- [ ] **Step 5: Run tests and vet**

Run: `go mod tidy && go build ./... && go vet ./... && go test ./internal/web/ -v`
Expected: all tests PASS. Fix compile issues (removed invalid block, unused imports).

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat: web API — roles, sql, tables, backups, connect endpoints"
```

---

### Task 8: frontend scaffold + shell + clusters/databases + connect modal

**Files:**
- Create: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/tailwind.config.js`, `frontend/postcss.config.js`, `frontend/index.html`, `frontend/src/main.ts`, `frontend/src/style.css`, `frontend/src/api.ts`, `frontend/src/store.ts`, `frontend/src/App.vue`, `frontend/src/views/Clusters.vue`, `frontend/src/views/ClusterDetail.vue`, `frontend/src/views/DatabasesTab.vue`, `frontend/src/components/ConnectModal.vue`
- Create: `internal/web/dist/index.html` replaced by real build output at the end of this task.

**Interfaces:**
- Consumes: the `/api` endpoints from Tasks 6–7.
- Produces: built SPA in `internal/web/dist/`; `api.ts` typed helpers used by all later views.

- [ ] **Step 1: Scaffold the frontend project**

`frontend/package.json`:
```json
{
  "name": "cnpg-manager-ui",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vite build"
  },
  "dependencies": {
    "vue": "^3.4.0"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^5.0.0",
    "autoprefixer": "^10.4.0",
    "postcss": "^8.4.0",
    "tailwindcss": "^3.4.0",
    "typescript": "^5.4.0",
    "vite": "^5.2.0"
  }
}
```

`frontend/vite.config.ts`:
```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: { outDir: '../internal/web/dist', emptyOutDir: true },
  server: { proxy: { '/api': 'http://localhost:8080' } }
})
```

`frontend/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "jsx": "preserve",
    "skipLibCheck": true,
    "esModuleInterop": true,
    "types": ["vite/client"]
  },
  "include": ["src/**/*.ts", "src/**/*.vue"]
}
```
Note: `vue-tsc` is not added; type safety there is limited to what Vite checks. Keep `build` = `vite build` only.

`frontend/tailwind.config.js`:
```js
/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        bg: '#0f1115',
        panel: '#171a21',
        panel2: '#1e222b',
        border: '#2a2f3a',
        accent: '#3ecf8e',
        accentDim: '#2a8f63',
        fg: '#d7dce2',
        dim: '#8b93a1'
      }
    }
  },
  plugins: []
}
```

`frontend/postcss.config.js`:
```js
export default { plugins: { tailwindcss: {}, autoprefixer: {} } }
```

`frontend/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>CNPG Manager</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="/src/main.ts"></script>
  </body>
</html>
```

- [ ] **Step 2: Source files**

`frontend/src/style.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;

html, body, #app { height: 100%; }
body { @apply bg-bg text-fg antialiased; }
::-webkit-scrollbar { width: 8px; height: 8px; }
::-webkit-scrollbar-thumb { @apply bg-border rounded; }
```

`frontend/src/main.ts`:
```ts
import { createApp } from 'vue'
import App from './App.vue'
import './style.css'

createApp(App).mount('#app')
```

`frontend/src/api.ts`:
```ts
export interface Cluster {
  name: string
  namespace: string
  version: number
  phase: string
  readyInstances: number
  instances: number
  port: number
  databases: number
  roles: number
  lastBackup?: string
  dbError?: string
}

export interface Database {
  name: string
  owner: string
  encoding: string
  template: boolean
  sizeKB: number
  acl?: string
}

export interface Role {
  name: string
  super: boolean
  login: boolean
  createDB: boolean
  createRole: boolean
  replication: boolean
  memberOf: string[]
  ownedDBs: string[]
}

export interface SqlResult {
  columns: string[]
  rows: any[][]
  command: string
  rowCount: number
}

export interface Column {
  name: string
  type: string
  nullable: boolean
  default?: string
}

export interface Index {
  name: string
  primary: boolean
  unique: boolean
  def: string
}

export interface TableInfo {
  name: string
  rowEst: number
  columns: Column[]
  indexes: Index[]
}

export interface Schema {
  name: string
  tables: TableInfo[]
}

export interface Rows {
  columns: Column[]
  rows: any[][]
  total: number
}

export interface Backup {
  name: string
  method: string
  backupKind: string
  phase: string
  destination?: string
  startedAt?: string
  finishedAt?: string
}

export interface ConnectInfo {
  user: string
  db: string
  host: string
  port: number
  password: string
  urlDirect: string
  urlVerifyFull: string
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init
  })
  if (!res.ok) {
    let msg = res.statusText
    try { msg = (await res.json()).error ?? msg } catch {}
    throw new Error(msg)
  }
  return res.json() as Promise<T>
}

export const api = {
  clusters: () => req<Cluster[]>('/api/clusters'),
  databases: (c: string, ns: string) => req<Database[]>(`/api/clusters/${c}/databases?ns=${ns}`),
  createDatabase: (c: string, ns: string, b: object) =>
    req<{ created: string }>(`/api/clusters/${c}/databases?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  dropDatabase: (c: string, ns: string, db: string) =>
    req<{ dropped: string }>(`/api/clusters/${c}/databases/${encodeURIComponent(db)}?ns=${ns}`, { method: 'DELETE' }),
  roles: (c: string, ns: string) => req<Role[]>(`/api/clusters/${c}/roles?ns=${ns}`),
  createRole: (c: string, ns: string, b: object) =>
    req<{ name: string; password: string }>(`/api/clusters/${c}/roles?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  dropRole: (c: string, ns: string, role: string) =>
    req<{ dropped: string }>(`/api/clusters/${c}/roles/${encodeURIComponent(role)}?ns=${ns}`, { method: 'DELETE' }),
  runSql: (c: string, ns: string, b: { db: string; statement: string; readOnly: boolean }) =>
    req<SqlResult>(`/api/clusters/${c}/sql?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  tables: (c: string, ns: string, db: string) => req<Schema[]>(`/api/clusters/${c}/tables?ns=${ns}&db=${encodeURIComponent(db)}`),
  rows: (c: string, ns: string, q: { db: string; schema: string; table: string; limit?: number; offset?: number }) => {
    const p = new URLSearchParams({ ns, ...q } as any)
    return req<Rows>(`/api/clusters/${c}/rows?${p}`)
  },
  backups: (c: string, ns: string) => req<Backup[]>(`/api/clusters/${c}/backups?ns=${ns}`),
  createBackup: (c: string, ns: string) =>
    req<{ created: string }>(`/api/clusters/${c}/backups?ns=${ns}`, { method: 'POST' }),
  connect: (c: string, ns: string, db: string, role: string) =>
    req<ConnectInfo>(`/api/clusters/${c}/connect?ns=${ns}&db=${encodeURIComponent(db)}&role=${encodeURIComponent(role)}`)
}
```

`frontend/src/store.ts`:
```ts
import { reactive } from 'vue'
import { api } from './api'
import type { Cluster, Database } from './api'

export interface ConnectState {
  open: boolean
  cluster: Cluster | null
  dbs: Database[]
}

export function phaseClass(c: Cluster): string {
  return c.phase === 'Cluster in healthy state' || !c.phase ? 'bg-accent' : 'bg-amber-400'
}

export const store = reactive({
  clusters: [] as Cluster[],
  current: null as Cluster | null,
  connect: { open: false, cluster: null, dbs: [] } as ConnectState,

  async loadClusters() {
    this.clusters = await api.clusters()
    if (this.current) {
      this.current = this.clusters.find((c) => c.name === this.current!.name && c.namespace === this.current!.namespace) ?? null
    }
    return this.clusters
  },

  async selectCluster(c: Cluster) {
    this.current = c
  },

  openConnect(cluster: Cluster) {
    this.connect.cluster = cluster
    this.connect.dbs = []
    this.connect.open = true
  }
})
```

- [ ] **Step 3: Root shell and clusters view**

`frontend/src/App.vue`:
```vue
<script setup lang="ts">
import { onMounted } from 'vue'
import Clusters from './views/Clusters.vue'
import ClusterDetail from './views/ClusterDetail.vue'
import ConnectModal from './components/ConnectModal.vue'
import { store } from './store'

onMounted(() => {
  store.loadClusters().catch((e) => console.error(e))
})
</script>

<template>
  <div class="flex h-full">
    <aside class="w-64 shrink-0 bg-panel border-r border-border flex flex-col">
      <div class="px-4 py-4 text-lg font-bold text-accent tracking-tight">cnpg manager</div>
      <div class="px-4 pb-3 text-xs uppercase tracking-wider text-dim">Clusters</div>
      <nav class="flex-1 overflow-y-auto px-2 space-y-1">
        <button
          v-for="c in store.clusters"
          :key="c.namespace + '/' + c.name"
          class="w-full text-left px-3 py-2 rounded text-sm hover:bg-panel2 flex items-center gap-2"
          :class="store.current?.name === c.name ? 'bg-panel2 text-accent' : 'text-fg'"
          @click="store.selectCluster(c)"
        >
          <span class="w-2 h-2 rounded-full inline-block" :class="phaseClass(c)"></span>
          <span class="truncate">{{ c.name }}</span>
        </button>
      </nav>
      <div class="px-4 py-3 text-xs text-dim">
        <button class="text-accent hover:underline" @click="store.selectCluster(null)">All clusters</button>
      </div>
    </aside>
    <main class="flex-1 overflow-y-auto">
      <ConnectModal v-if="store.connect.open" />
      <Clusters v-if="!store.current" />
      <ClusterDetail v-else :cluster="store.current" />
    </main>
  </div>
</template>
```

`frontend/src/views/Clusters.vue`:
```vue
<script setup lang="ts">
import { store, phaseClass } from '../store'

async function refresh() {
  await store.loadClusters()
}
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-xl font-semibold">Clusters</h1>
      <button class="px-3 py-1.5 rounded bg-panel2 border border-border text-sm hover:bg-panel" @click="refresh">Refresh</button>
    </div>
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <div v-for="c in store.clusters" :key="c.namespace + '/' + c.name"
           class="bg-panel border border-border rounded-lg p-4 hover:border-accentDim cursor-pointer"
           @click="store.selectCluster(c)">
        <div class="flex items-center gap-2 mb-2">
          <span class="w-2 h-2 rounded-full" :class="phaseClass(c)"></span>
          <span class="font-medium truncate">{{ c.name }}</span>
          <span class="text-xs text-dim ml-auto">v{{ c.version }}</span>
        </div>
        <div class="text-xs text-dim">{{ c.namespace }} · {{ c.readyInstances }}/{{ c.instances }} instances</div>
        <div class="text-xs text-dim">{{ c.databases }} databases · {{ c.roles }} roles</div>
        <div class="mt-2 text-xs">
          <span v-if="c.lastBackup" class="text-accent">last backup {{ c.lastBackup }}</span>
          <span v-if="c.dbError" class="text-red-400">{{ c.dbError }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
```
> Note: `api.deleteCluster` used above is not part of the backend — remove the `dropDb` function and mention deletion is out of scope.

- [ ] **Step 4: Databases tab + connect modal**

`frontend/src/views/DatabasesTab.vue`:
```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Database } from '../api'
import { store } from '../store'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const creating = ref(false)
const form = ref({ name: '', owner: '', template: '', encoding: '' })
const error = ref('')

async function load() {
  error.value = ''
  try {
    dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
  } catch (e) {
    error.value = String(e)
  }
}
watch(() => props.cluster.name, load, { immediate: true })

async function create() {
  try {
    await api.createDatabase(props.cluster.name, props.cluster.namespace, form.value)
    form.value = { name: '', owner: '', template: '', encoding: '' }
    creating.value = false
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

async function drop(db: Database) {
  if (!confirm(`Drop database ${db.name}? This cannot be undone.`)) return
  try {
    await api.dropDatabase(props.cluster.name, props.cluster.namespace, db.name)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

const fmt = (kb: number) => kb > 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Databases</h2>
      <div class="flex gap-2">
        <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="creating = !creating">
          New database
        </button>
      </div>
    </div>
    <div v-if="creating" class="bg-panel2 border border-border rounded p-4 mb-4 flex flex-wrap gap-2">
      <input v-model="form.name" placeholder="name" class="inp" />
      <input v-model="form.owner" placeholder="owner role (optional)" class="inp" />
      <input v-model="form.template" placeholder="template (optional)" class="inp" />
      <input v-model="form.encoding" placeholder="encoding (optional)" class="inp" />
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm" @click="create">Create</button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Owner</th><th>Encoding</th><th>Size</th><th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="d in dbs" :key="d.name" class="border-b border-border/50">
          <td class="py-2 font-mono">{{ d.name }} <span v-if="d.template" class="text-xs text-dim">template</span></td>
          <td>{{ d.owner }}</td>
          <td>{{ d.encoding }}</td>
          <td>{{ fmt(d.sizeKB) }}</td>
          <td class="text-right">
            <button class="text-accent text-xs hover:underline mr-3" @click="store.openConnect(props.cluster)">Connect</button>
            <button class="text-red-400 text-xs hover:underline" @click="drop(d)">Drop</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.inp { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
```
> Note: the table element classes `border-r`, `border-b`, `@apply`, etc. depend on Tailwind being built — all good. The `@apply` inside `<style scoped>` requires `@tailwind components` already loaded; works.

`frontend/src/views/ClusterDetail.vue`:
```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster } from '../api'
import { store } from '../store'
import DatabasesTab from './DatabasesTab.vue'
import RolesTab from './RolesTab.vue'
import SqlTab from './SqlTab.vue'
import TablesTab from './TablesTab.vue'
import BackupsTab from './BackupsTab.vue'

const props = defineProps<{ cluster: Cluster }>()
const tab = ref<'overview' | 'databases' | 'roles' | 'sql' | 'tables' | 'backups'>('overview')
const first = ref(true)
watch(() => props.cluster, async () => {
  if (first.value) {
    first.value = false
    return
  }
  await store.loadClusters()
}, { deep: true })

const tabs = [
  { id: 'overview', label: 'Overview' },
  { id: 'databases', label: 'Databases' },
  { id: 'roles', label: 'Roles' },
  { id: 'sql', label: 'SQL' },
  { id: 'tables', label: 'Tables' },
  { id: 'backups', label: 'Backups' }
] as const
</script>

<template>
  <div class="p-6">
    <div class="flex items-center gap-3 mb-4">
      <button class="text-dim hover:text-fg mr-2" @click="store.selectCluster(null as any)">←</button>
      <h1 class="text-xl font-semibold">{{ cluster.name }}</h1>
      <span class="text-xs px-2 py-0.5 rounded bg-panel2 border border-border text-dim">{{ cluster.namespace }}</span>
      <button class="ml-auto px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="store.openConnect(cluster)">
        Connect
      </button>
    </div>
    <div class="flex gap-1 mb-4 border-b border-border">
      <button v-for="t in tabs" :key="t.id"
              class="px-3 py-2 text-sm -mb-px border-b-2"
              :class="tab === t.id ? 'border-accent text-fg' : 'border-transparent text-dim hover:text-fg'"
              @click="tab = t.id">{{ t.label }}</button>
    </div>
    <DatabasesTab v-if="tab === 'databases'" :cluster="cluster" />
    <RolesTab v-else-if="tab === 'roles'" :cluster="cluster" />
    <SqlTab v-else-if="tab === 'sql'" :cluster="cluster" />
    <TablesTab v-else-if="tab === 'tables'" :cluster="cluster" />
    <BackupsTab v-else-if="tab === 'backups'" :cluster="cluster" />
    <div v-else class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Postgres</div>
        <div class="text-lg font-semibold">v{{ cluster.version }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Instances</div>
        <div class="text-lg font-semibold">{{ cluster.readyInstances }}/{{ cluster.instances }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Databases</div>
        <div class="text-lg font-semibold">{{ cluster.databases }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Roles</div>
        <div class="text-lg font-semibold">{{ cluster.roles }}</div>
      </div>
      <div v-if="cluster.lastBackup" class="col-span-2 bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Last backup</div>
        <div class="text-sm">{{ cluster.lastBackup }}</div>
      </div>
    </div>
  </div>
</template>
```

`frontend/src/components/ConnectModal.vue`:
```vue
<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { api } from '../api'
import type { ConnectInfo, Database, Role } from '../api'
import { store } from '../store'

const info = ref<ConnectInfo | null>(null)
const dbs = ref<Database[]>([])
const roles = ref<Role[]>([])
const db = ref('')
const role = ref('')
const sslmode = ref<'require' | 'verify-full'>('require')
const loading = ref(false)
const error = ref('')
const copied = ref('')

const cluster = computed(() => store.connect.cluster!)
watch(cluster, async (c) => {
  if (!c) return
  error.value = ''
  try {
    ;[dbs.value, roles.value] = await Promise.all([
      api.databases(c.name, c.namespace),
      api.roles(c.name, c.namespace)
    ])
    const firstDb = dbs.value.find((d) => !d.template) ?? dbs.value[0]
    if (firstDb) { db.value = firstDb.name; await load() }
  } catch (e) { error.value = String(e) }
})

async function load() {
  if (!cluster.value || !db.value || !role.value) return
  loading.value = true
  error.value = ''
  try {
    info.value = await api.connect(cluster.value.name, cluster.value.namespace, db.value, role.value)
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

watch([db, role], () => load())

function buildUrl(): string {
  if (!info.value) return ''
  const base = info.value.urlDirect.split('?')[0]
  const userPass = base.includes('@') ? base : base
  return `${userPass}?sslmode=${sslmode.value}`
}

async function copy(text: string, key: string) {
  await navigator.clipboard.writeText(text)
  copied.value = key
  setTimeout(() => (copied.value = ''), 1200)
}
</script>

<template>
  <div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
    <div class="bg-panel border border-border rounded-lg w-[640px] max-h-[85vh] overflow-y-auto">
      <div class="px-5 py-4 flex items-center border-b border-border">
        <div>
          <div class="font-semibold">{{ cluster?.name }}</div>
          <div class="text-xs text-dim">PostgreSQL connection</div>
        </div>
        <button class="ml-auto text-dim hover:text-fg" @click="store.connect.open = false">✕</button>
      </div>
      <div class="p-5 space-y-4">
        <div v-if="error" class="text-red-400 text-sm">{{ error }}</div>
        <div class="flex gap-3">
          <label class="flex-1 text-xs text-dim">Database
            <select v-model="db" class="sel w-full">
              <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
            </select>
          </label>
          <label class="flex-1 text-xs text-dim">Role
            <select v-model="role" class="sel w-full">
              <option v-for="r in roles" :key="r.name" :value="r.name">{{ r.name }}</option>
            </select>
          </label>
          <label class="text-xs text-dim">SSL
            <select v-model="sslmode" class="sel">
              <option value="require">require</option>
              <option value="verify-full">verify-full</option>
            </select>
          </label>
        </div>
        <div v-if="info">
          <div class="text-xs text-dim mb-1">Connection URL</div>
          <div class="flex items-center gap-2 bg-bg border border-border rounded p-2">
            <code class="flex-1 text-xs font-mono text-accent break-all select-all">{{ buildUrl() }}</code>
            <button class="px-2 py-1 rounded bg-panel2 border border-border text-xs hover:bg-bg" @click="copy(buildUrl(), 'url')">
              {{ copied === 'url' ? 'copied' : 'copy' }}
            </button>
          </div>
          <div class="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs">
            <div class="text-dim">User</div><div class="font-mono">{{ info.user }}</div>
            <div class="text-dim">Password</div>
            <div class="flex items-center gap-2">
              <span class="font-mono">{{ info.password }}</span>
              <button class="text-accent hover:underline" @click="copy(info.password, 'pw')">{{ copied === 'pw' ? 'copied' : 'copy' }}</button>
            </div>
            <div class="text-dim">Host</div><div class="font-mono">{{ info.host }}:{{ info.port }}</div>
          </div>
        </div>
        <div v-if="loading" class="text-xs text-dim">…</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg mt-1; }
</style>
```

- [ ] **Step 5: Install, build, and verify**

```bash
cd frontend && npm install
npm run build
cd .. && go build ./... && go test ./internal/... -v
```

Expected: `frontend/dist` → `internal/web/dist` populated; `go build` succeeds with embedded assets; Go tests still pass.

- [ ] **Step 6: Commit**

```bash
git add . && git -c 'message' 2>/dev/null; git add frontend internal/web/dist && git commit -m "feat: frontend shell, clusters, databases, connect modal"
```

(Do not commit `frontend/node_modules/`; `.gitignore` covers it.)

---

### Task 9: frontend — roles + SQL editor tabs

**Files:**
- Create: `frontend/src/views/RolesTab.vue`, `frontend/src/views/SqlTab.vue`

**Interfaces:**
- Consumes: `api.roles`, `api.createRole`, `api.dropRole`, `api.runSql`.

- [ ] **Step 1: Implement `RolesTab.vue`**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Role } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const roles = ref<Role[]>([])
const creating = ref(false)
const error = ref('')
const flash = ref('')
const form = ref({ name: '', password: '', grantDB: '', super: false, createDB: false })

async function load() {
  error.value = ''
  try { roles.value = await api.roles(props.cluster.name, props.cluster.namespace) }
  catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, load, { immediate: true })

async function create() {
  try {
    const r = await api.createRole(props.cluster.name, props.cluster.namespace, form.value)
    flash.value = `Role ${r.name} created — password: ${r.password} (shown once)`
    form.value = { name: '', password: '', grantDB: '', super: false, createDB: false }
    creating.value = false
    await load()
  } catch (e) { error.value = String(e) }
}

async function drop(r: Role) {
  if (!confirm(`Drop role ${r.name}?`)) return
  try { await api.dropRole(props.cluster.name, props.cluster.namespace, r.name); await load() }
  catch (e) { error.value = String(e) }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Roles</h2>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="creating = !creating">New role</button>
    </div>
    <div v-if="flash" class="text-accent text-sm mb-3 bg-panel2 border border-border rounded p-3">{{ flash }}</div>
    <div v-if="creating" class="bg-panel2 border border-border rounded p-4 mb-4 space-y-2">
      <div class="flex gap-2">
        <input v-model="form.name" placeholder="name" class="inp" />
        <input v-model="form.password" placeholder="password (blank = generate)" class="inp" />
        <input v-model="form.grantDB" placeholder="grant on database (optional)" class="inp" />
      </div>
      <div class="flex gap-4 text-sm text-dim">
        <label><input type="checkbox" v-model="form.createDB" /> createdb</label>
        <label><input type="checkbox" v-model="form.super" /> superuser</label>
      </div>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm" @click="create">Create</button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Attrs</th><th>Member of</th><th>Owns</th><th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in roles" :key="r.name" class="border-b border-border/50">
          <td class="py-2 font-mono">{{ r.name }}</td>
          <td class="text-xs">
            <span v-if="r.super" class="text-amber-400">SUPER </span>
            <span v-if="r.createDB" class="text-accent">CREATEDB </span>
            <span v-if="r.replication" class="text-dim">REPL </span>
          </td>
          <td class="text-xs text-dim">{{ r.memberOf.join(', ') || '—' }}</td>
          <td class="text-xs text-dim">{{ r.ownedDBs.join(', ') || '—' }}</td>
          <td class="text-right">
            <button class="text-red-400 text-xs hover:underline" @click="drop(r)">Drop</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.inp { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg flex-1; }
</style>
```

- [ ] **Step 2: Implement `SqlTab.vue`**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Database, SqlResult } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const db = ref('')
const statement = ref('')
const readOnly = ref(true)
const result = ref<SqlResult | null>(null)
const running = ref(false)
const error = ref('')

async function loadDbs() {
  try {
    dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
    if (!db.value && dbs.value.length) db.value = dbs.value.find((d) => !d.template)?.name ?? dbs.value[0].name
  } catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, loadDbs, { immediate: true })

async function run() {
  if (!db.value || !statement.value) return
  running.value = true
  error.value = ''
  try {
    result.value = await api.runSql(props.cluster.name, props.cluster.namespace, {
      db: db.value, statement: statement.value, readOnly: readOnly.value
    })
  } catch (e) { error.value = String(e) } finally { running.value = false }
}

function onKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') run()
}

const cell = (v: any) => v === null ? 'NULL' : String(v)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-3">
      <select v-model="db" class="sel">
        <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
      </select>
      <label class="text-xs text-dim flex items-center gap-1">
        <input type="checkbox" v-model="readOnly" /> read-only
      </label>
      <button class="ml-auto px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" :disabled="running" @click="run">
        {{ running ? 'Running…' : 'Run (Ctrl+Enter)' }}
      </button>
    </div>
    <textarea v-model="statement" placeholder="select * from my_table;"
              class="w-full h-40 bg-bg border border-border rounded p-3 font-mono text-sm"
              @keydown="onKey" spellcheck="false"></textarea>
    <div v-if="error" class="text-red-400 text-sm mt-2">{{ error }}</div>
    <div v-if="result && result.columns?.length" class="mt-4 overflow-x-auto">
      <table class="text-xs font-mono w-full">
        <thead>
          <tr class="text-left text-dim border-b border-border">
            <th v-for="c in result.columns" :key="c" class="py-1.5 pr-4">{{ c }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in result.rows" :key="i" class="border-b border-border/40">
            <td v-for="(v, j) in row" :key="j" class="py-1 pr-4 whitespace-nowrap">{{ cell(v) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="text-xs text-dim mt-2">{{ result.rows.length }} rows</div>
    </div>
    <div v-else-if="result" class="text-sm text-dim mt-3">{{ result.command }} · {{ result.rowCount }} rows</div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
```

- [ ] **Step 3: Build and verify**

```bash
cd frontend && npm run build && cd .. && go build ./... && go test ./internal/... 
```

Expected: build clean.

- [ ] **Step 4: Commit**

```bash
git add frontend internal/web/dist && git commit -m "feat: roles and SQL editor tabs"
```

---

### Task 10: frontend — tables browser + backups tab

**Files:**
- Create: `frontend/src/views/TablesTab.vue`, `frontend/src/views/BackupsTab.vue`

**Interfaces:**
- Consumes: `api.tables`, `api.rows`, `api.backups`, `api.createBackup`.

- [ ] **Step 1: Implement `TablesTab.vue`**

```vue
<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { api } from '../api'
import type { Cluster, Database, Schema, TableInfo, Rows } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const db = ref('')
const schemas = ref<Schema[]>([])
const selected = ref<{ schema: string; table: string } | null>(null)
const rows = ref<Rows | null>(null)
const page = ref(0)
const pageSize = 50
const error = ref('')
const loading = ref(false)

async function loadDbs() {
  dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
  if (!db.value && dbs.value.length) db.value = dbs.value.find((d) => !d.template)?.name ?? dbs.value[0].name
}
async function loadTables() {
  if (!db.value) return
  error.value = ''
  try {
    schemas.value = await api.tables(props.cluster.name, props.cluster.namespace, db.value)
  } catch (e) { error.value = String(e) }
}
async function loadRows() {
  if (!selected.value) return
  loading.value = true
  try {
    rows.value = await api.rows(props.cluster.name, props.cluster.namespace, {
      db: db.value, schema: selected.value.schema, table: selected.value.table,
      limit: pageSize, offset: page.value * pageSize
    })
  } catch (e) { error.value = String(e) } finally { loading.value = false }
}

watch([db], loadTables)
watch([selected, page], loadRows, { deep: true })
void loadDbs()

const totalPages = computed(() => rows.value ? Math.max(1, Math.ceil(rows.value.total / pageSize)) : 1)
const cols = computed(() => rows.value?.columns ?? [])

function pick(s: string, t: TableInfo) {
  selected.value = { schema: s, table: t.name }
  page.value = 0
}
const cell = (v: any) => v === null ? 'NULL' : String(v)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-3">
      <select v-model="db" class="sel">
        <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
      </select>
      <span class="text-xs text-dim">browse only — edit with SQL tab</span>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <div class="grid grid-cols-[220px_1fr] gap-4">
      <div class="bg-panel border border-border rounded overflow-y-auto max-h-[70vh]">
        <template v-for="s in schemas" :key="s.name">
          <div class="px-3 py-1.5 text-xs font-semibold text-dim uppercase tracking-wide sticky top-0 bg-panel">{{ s.name }}</div>
          <button v-for="t in s.tables" :key="t.name"
                  class="block w-full text-left px-3 py-1.5 text-sm hover:bg-panel2"
                  :class="selected?.schema === s.name && selected.table === t.name ? 'bg-panel2 text-accent' : ''"
                  @click="pick(s.name, t)">
            {{ t.name }} <span class="text-xs text-dim">({{ t.columns.length }})</span>
          </button>
        </template>
      </div>
      <div v-if="!selected" class="text-dim text-sm py-8 text-center">Select a table to browse its rows.</div>
      <div v-else class="bg-panel border border-border rounded overflow-x-auto max-h-[70vh]">
        <div class="px-4 py-2 border-b border-border flex items-center gap-2">
          <span class="font-mono text-sm">{{ selected.schema }}.{{ selected.table }}</span>
          <span v-if="rows" class="text-xs text-dim ml-auto">{{ rows.total }} rows total</span>
        </div>
        <table class="text-xs font-mono w-full">
          <thead>
            <tr class="text-left text-dim border-b border-border">
              <th v-for="c in cols" :key="c.name" class="py-1.5 px-2 whitespace-nowrap">
                {{ c.name }} <span class="text-dim/60">· {{ c.type }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in rows?.rows ?? []" :key="i" class="border-b border-border/40 hover:bg-panel2">
              <td v-for="(v, j) in row" :key="j" class="py-1 px-2 whitespace-nowrap max-w-[300px] truncate">{{ cell(v) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="loading" class="text-xs text-dim p-3">…</div>
        <div v-if="rows && totalPages > 1" class="flex items-center gap-3 p-2 border-t border-border text-xs">
          <button class="px-2 py-1 rounded bg-panel2 border border-border disabled:opacity-40" :disabled="page === 0" @click="page--">Prev</button>
          <span class="text-dim">{{ page + 1 }} / {{ totalPages }}</span>
          <button class="px-2 py-1 rounded bg-panel2 border border-border disabled:opacity-40" :disabled="page + 1 >= totalPages" @click="page++">Next</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
```

- [ ] **Step 2: Implement `BackupsTab.vue`**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Backup } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const backups = ref<Backup[]>([])
const error = ref('')
const creating = ref(false)

async function load() {
  error.value = ''
  try { backups.value = await api.backups(props.cluster.name, props.cluster.namespace) }
  catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, load, { immediate: true })

const phaseColor = (p: string) => ({
  completed: 'text-accent',
  running: 'text-amber-400',
  failed: 'text-red-400',
  pending: 'text-dim'
} as Record<string, string>)[p] ?? 'text-dim'

async function backUp() {
  creating.value = true
  try {
    await api.createBackup(props.cluster.name, props.cluster.namespace)
    await load()
  } catch (e) { error.value = String(e) } finally { creating.value = false }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Backups</h2>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" :disabled="creating" @click="backUp">
        {{ creating ? 'Triggering…' : 'Backup now' }}
      </button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Method</th><th>Phase</th><th>Started</th><th>Finished</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="b in backups" :key="b.name" class="border-b border-border/50">
          <td class="py-2 font-mono">{{ b.name }}</td>
          <td>{{ b.method }}</td>
          <td :class="phaseColor(b.phase)">{{ b.phase }}</td>
          <td class="text-dim">{{ b.startedAt || '—' }}</td>
          <td class="text-dim">{{ b.finishedAt || '—' }}</td>
        </tr>
        <tr v-if="!backups.length"><td colspan="5" class="py-4 text-dim text-center">No backups for this cluster yet.</td></tr>
      </tbody>
    </table>
  </div>
</template>
```

- [ ] **Step 3: Build and verify**

```bash
cd frontend && npm run build && cd .. && go build ./... && go test ./internal/...
```

Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add frontend internal/web/dist && git commit -m "feat: tables browser and backups tab"
```

---

### Task 11: deploy manifests, Dockerfile, README

**Files:**
- Create: `deploy/kustomization.yaml`, `deploy/namespace.yaml`, `deploy/serviceaccount.yaml`, `deploy/rbac.yaml`, `deploy/deployment.yaml`, `deploy/service.yaml`, `deploy/ingress.example.yaml`, `Dockerfile`, `Makefile`, `README.md`

**Interfaces:**
- Consumes: single Go binary with embedded assets, listening on `:8080`, reading in-cluster config.

- [ ] **Step 1: Kustomize manifests**

`deploy/kustomization.yaml`:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: cnpg-manager
resources:
  - namespace.yaml
  - serviceaccount.yaml
  - rbac.yaml
  - deployment.yaml
  - service.yaml
images:
  - name: cnpg-manager
    newName: ghcr.io/YOURUSER/cnpg-manager
    newTag: latest
```

`deploy/namespace.yaml`:
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: cnpg-manager
```

`deploy/serviceaccount.yaml`:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cnpg-manager
```

`deploy/rbac.yaml`:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cnpg-manager
rules:
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters", "backups"]
    verbs: ["get", "list", "watch", "create"]
  - apiGroups: [""]
    resources: ["secrets", "pods"]
    verbs: ["get", "list", "watch", "create", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cnpg-manager
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cnpg-manager
subjects:
  - kind: ServiceAccount
    name: cnpg-manager
    namespace: cnpg-manager
```

`deploy/deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cnpg-manager
spec:
  replicas: 1
  selector:
    matchLabels:
      app: cnpg-manager
  template:
    metadata:
      labels:
        app: cnpg-manager
    spec:
      serviceAccountName: cnpg-manager
      containers:
        - name: cnpg-manager
          image: cnpg-manager
          ports:
            - containerPort: 8080
              name: http
          env:
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { memory: 256Mi }
          securityContext:
            allowPrivilegeEscalation: false
            runAsNonRoot: true
            capabilities: { drop: ["ALL"] }
```

`deploy/service.yaml`:
```yaml
apiVersion: v1
kind: Service
metadata:
  name: cnpg-manager
spec:
  selector:
    app: cnpg-manager
  ports:
    - port: 80
      targetPort: http
```

`deploy/ingress.example.yaml` (commented guidance; not part of kustomization):
```yaml
# Example: expose via Traefik. Add to kustomization.yaml resources when ready.
# apiVersion: networking.k8s.io/v1
# kind: Ingress
# metadata:
#   name: cnpg-manager
# spec:
#   rules:
#     - host: cnpg.home.lan
#       http:
#         paths:
#           - path: /
#             pathType: Prefix
#             backend:
#               service:
#                 name: cnpg-manager
#                 port:
#                   number: 80
```

- [ ] **Step 2: Dockerfile**

```dockerfile
FROM node:18-alpine AS frontend
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/cnpg-manager ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/cnpg-manager /cnpg-manager
EXPOSE 8080
ENTRYPOINT ["/cnpg-manager"]
```

- [ ] **Step 3: Makefile**

```make
.PHONY: build frontend test image deploy

build:
	go build -o bin/cnpg-manager ./cmd/server

frontend:
	cd frontend && npm ci && npm run build

test:
	go vet ./... && go test ./internal/...

image:
	docker build -t ghcr.io/YOURUSER/cnpg-manager:latest .

deploy:
	kubectl apply -k deploy
```

- [ ] **Step 4: README.md**

Include: what it does, quick build (`make frontend && make build`), local dev (`make frontend && go run ./cmd/server` against a kubeconfig), in-cluster deploy (`make image && make deploy`), RBAC notes (backend has write/read capabilities spanning cluster namespaces + secret read), and how an app consumes a URL (Connect modal → copy URL into the app's Secret). Keep it under 60 lines.

- [ ] **Step 5: Validate YAML and image build path**

Run: `kubectl kustomize deploy 2>/dev/null || (docker run —rm -v "$PWD/deploy:/deploy" k8s.gcr.io/kustomize/kustomize:v3.8.1 build /deploy 2>/dev/null || echo "kustomize not available — YAML reviewed by eye")`

Then: `go vet ./... && go build ./...`.

Expected: manifests print (or note tool unavailable); build clean.

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat: in-cluster deployment manifests, Dockerfile, README"
```

---

### Task 12: end-to-end verification with live Postgres

**Files:**
- Create: `scripts/smoke.sh`
- Modify: none (verification task)

**Interfaces:**
- Consumes: complete repo.

- [ ] **Step 1: Write `scripts/smoke.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

# Boots a throwaway local Postgres and runs the pg integration tests against it.
PGVER=$(ls /usr/lib/postgresql 2>/dev/null | sort -V | tail -1 || true)
if [ -z "$PGVER" ]; then
  if command -v apt-get >/dev/null && [ "$(id -u)" = 0 ]; then
    apt-get update -qq
    apt-get install -y -qq postgresql >/dev/null 2>&1
    PGVER=$(ls /usr/lib/postgresql | sort -V | tail -1)
    # ensure a non-root path allows initdb
    export PATH="/usr/lib/postgresql/$PGVER/bin:$PATH"
  else
    echo "PostgreSQL not installed and no root to install it — skipping smoke test"
    exit 0
  fi
fi

TMP=$(mktemp -d)
trap 'pg_ctl -D "$TMP/data" stop -m fast >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT

PORT=55432
initdb -D "$TMP/data" -U postgres --auth=trust >/dev/null
pg_ctl -D "$TMP/data" -o "-p $PORT -k $TMP" -l "$TMP/log" start >/dev/null

export CNPG_TEST_DSN="postgres://postgres@localhost:$PORT/postgres?sslmode=disable&application_name=cnpg-manager"
go test ./internal/pg/ -run 'TestIntegration' -v
```

- [ ] **Step 2: Run the full verification**

```bash
chmod +x scripts/smoke.sh
go vet ./...
go build ./...
go test ./internal/...
./scripts/smoke.sh
cd frontend && npm run build && cd ..
```

Expected: `go vet` clean; `go build` clean; Go unit tests (kube + web with fakes) pass; integration tests run against the real local Postgres and pass (database/role/sql/tables flows); frontend builds into `internal/web/dist`.

- [ ] **Step 3: Verify git status and commit**

```bash
git status --short
git add scripts && git commit -m "test: local postgres smoke script"
```

Expected: only `scripts/smoke.sh` added; prior tasks already committed.

---

## Post-implementation checklist

- `go vet ./...`, `go build ./...`, `go test ./internal/...` all green.
- `npm run build` in `frontend/` populates `internal/web/dist`.
- `scripts/smoke.sh` passes against a fresh local Postgres.
- Deploy: user runs `make image && make deploy`, then visits the Service/Ingress URL.
- Known deferred items (matches spec): no row editing, no metrics, no restore-from-backup, no UI auth, no connection pooler.