package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"cnpg-manager/internal/pg"
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

func (f *fakeStore) ListClusters(ctx context.Context) ([]apiv1.Cluster, error) {
	return f.clusters, nil
}
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
func (f *fakeStore) DeleteSecret(ctx context.Context, ns, name string) error {
	return nil
}

func newTestHandler(cs ClusterStore, pgc PGFunc) http.Handler {
	return New(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) {
		if pgc == nil {
			return &fakePG{}, nil
		}
		return pgc(ctx, cl)
	})
}

func TestListClusters(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"},
			Spec: apiv1.ClusterSpec{ImageName: "ghcr.io/cloudnative-pg/postgresql:17.4"}},
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
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) {
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
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) { return pgc, nil })
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
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) { return pgc, nil })
	req := httptest.NewRequest("DELETE", "/api/clusters/pg1/databases/app?ns=db", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || len(pgc.dropped) != 1 || pgc.dropped[0] != "app" {
		t.Fatalf("drop failed: code=%d dropped=%v", rec.Code, pgc.dropped)
	}
}

func TestGetClusterNotFound(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}}
	h := newTestHandler(cs, nil)
	req := httptest.NewRequest("GET", "/api/clusters/nope?ns=db", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetClusterAmbiguous(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "a"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "b"}},
	}}
	h := newTestHandler(cs, nil)
	req := httptest.NewRequest("GET", "/api/clusters/pg1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetClusterDetail(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"},
			Spec: apiv1.ClusterSpec{ImageName: "ghcr.io/cloudnative-pg/postgresql:17.4"}},
	}}
	pgc := &fakePG{dbs: []pg.DBInfo{{Name: "app"}}}
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) { return pgc, nil })
	req := httptest.NewRequest("GET", "/api/clusters/pg1?ns=db", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out clusterView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Databases != 1 || out.Name != "pg1" || out.Version != 17 {
		t.Fatalf("unexpected %+v", out)
	}
}

func TestSPAFallback(t *testing.T) {
	cs := &fakeStore{}
	h := newTestHandler(cs, nil)
	req := httptest.NewRequest("GET", "/some/client/route", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "cnpg-manager") {
		t.Fatalf("expected SPA fallback, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPINotFoundIsJSON(t *testing.T) {
	cs := &fakeStore{}
	h := newTestHandler(cs, nil)
	req := httptest.NewRequest("GET", "/api/clusters/pg1/nonexistent-endpoint", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %q: %s", ct, rec.Body.String())
	}
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateRoleReturnsPasswordAndSecret(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}, secret: map[string][]byte{"password": []byte("old")}}
	pgc := &fakePG{}
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) { return pgc, nil })
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
}

func TestRunSQLValidation(t *testing.T) {
	cs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}}
	h := newTestHandler(cs, func(ctx context.Context, cl *apiv1.Cluster) (PG, error) {
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
