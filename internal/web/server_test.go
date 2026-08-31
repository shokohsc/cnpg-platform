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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	crds     map[string]*unstructured.Unstructured
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

func (f *fakeStore) ListCRD(ctx context.Context, kind, ns string) ([]unstructured.Unstructured, error) {
	var out []unstructured.Unstructured
	for _, o := range f.crds {
		if k, _, _ := unstructured.NestedString(o.Object, "kind"); k != kind {
			continue
		}
		if ns != "" {
			if o.GetNamespace() != ns {
				continue
			}
		}
		out = append(out, *o)
	}
	return out, nil
}
func (f *fakeStore) GetCRD(ctx context.Context, kind, ns, name string) (*unstructured.Unstructured, error) {
	for _, o := range f.crds {
		if o.GetName() == name && (ns == "" || o.GetNamespace() == ns) {
			cp := o.DeepCopy()
			return cp, nil
		}
	}
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "crds"}, name)
}
func (f *fakeStore) CreateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	cp := obj.DeepCopy()
	if ns != "" && cp.GetNamespace() == "" {
		cp.SetNamespace(ns)
	}
	cp.SetResourceVersion("1")
	if f.crds == nil {
		f.crds = map[string]*unstructured.Unstructured{}
	}
	f.crds[cp.GetNamespace()+"/"+cp.GetName()] = cp
	return nil
}
func (f *fakeStore) UpdateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	if obj.GetResourceVersion() == "" {
		return apierrors.NewConflict(schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "crds"}, obj.GetName(), nil)
	}
	f.crds[obj.GetNamespace()+"/"+obj.GetName()] = obj.DeepCopy()
	return nil
}
func (f *fakeStore) PatchCRD(ctx context.Context, kind, ns, name string, patch map[string]any) error {
	for _, o := range f.crds {
		if o.GetName() == name && (ns == "" || o.GetNamespace() == ns) {
			pb, _ := json.Marshal(patch)
			mb, _ := o.MarshalJSON()
			merged, err := mergeJSON(mb, pb)
			if err != nil {
				return err
			}
			mergedObj := &unstructured.Unstructured{}
			if err := mergedObj.UnmarshalJSON(merged); err != nil {
				return err
			}
			f.crds[o.GetNamespace()+"/"+o.GetName()] = mergedObj
			return nil
		}
	}
	return apierrors.NewNotFound(schema.GroupResource{Group: "postgresql.cnpg.io", Resource: "crds"}, name)
}
func (f *fakeStore) DeleteCRD(ctx context.Context, kind, ns, name string) error {
	delete(f.crds, ns+"/"+name)
	return nil
}

func mergeJSON(base, patch []byte) ([]byte, error) {
	var b, p map[string]any
	if err := json.Unmarshal(base, &b); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(patch, &p); err != nil {
		return nil, err
	}
	deepMerge(b, p)
	return json.Marshal(b)
}

func deepMerge(base, patch map[string]any) {
	for k, pv := range patch {
		if pv == nil {
			delete(base, k)
			continue
		}
		bv, ok := base[k].(map[string]any)
		pm, ok2 := pv.(map[string]any)
		if ok && ok2 {
			deepMerge(bv, pm)
		} else {
			base[k] = pv
		}
	}
}

func seedCRD(f *fakeStore, kind, ns, name string) {
	cp := &unstructured.Unstructured{}
	cp.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: kind})
	cp.SetName(name)
	cp.SetNamespace(ns)
	cp.Object["spec"] = map[string]any{"instances": int64(1)}
	_ = f.CreateCRD(context.Background(), kind, ns, cp)
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

func TestCRDInvalidKind(t *testing.T) {
	h := newTestHandler(&fakeStore{}, nil)
	req := httptest.NewRequest("GET", "/api/crds/Bogus", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for bogus kind, got %d", rec.Code)
	}
}

func TestCRDListAndGet(t *testing.T) {
	fs := &fakeStore{}
	seedCRD(fs, "Backup", "db", "b1")
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/crds/Backup?ns=db", nil))
	if rec.Code != 200 {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/api/crds/Backup/b1?ns=db", nil))
	if rec2.Code != 200 {
		t.Fatalf("get status %d", rec2.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &got)
	meta := got["metadata"].(map[string]any)
	if meta["name"] != "b1" {
		t.Fatalf("bad body %v", got)
	}
}

func TestCRDClusterScopedListAllNamespaces(t *testing.T) {
	fs := &fakeStore{}
	seedCRD(fs, "ClusterImageCatalog", "", "cat")
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/crds/ClusterImageCatalog", nil))
	if rec.Code != 200 {
		t.Fatalf("list status %d: %s", rec.Code, rec.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cluster-scoped crd, got %d", len(got))
	}
}

func TestCRDCreatePatchDelete(t *testing.T) {
	fs := &fakeStore{}
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/api/crds/Backup?ns=db",
		strings.NewReader(`{"name":"b9","spec":{"method":"barmanObjectStore"}}`)))
	if rec.Code != 201 {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("PATCH", "/api/crds/Backup/b9?ns=db",
		strings.NewReader(`{"spec":{"phase":"Running"}}`)))
	if rec2.Code != 200 {
		t.Fatalf("patch status %d: %s", rec2.Code, rec2.Body.String())
	}
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest("DELETE", "/api/crds/Backup/b9?ns=db", nil))
	if rec3.Code != 200 {
		t.Fatalf("delete status %d", rec3.Code)
	}
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, httptest.NewRequest("GET", "/api/crds/Backup/b9?ns=db", nil))
	if rec4.Code != 404 {
		t.Fatalf("expected 404 after delete, got %d", rec4.Code)
	}
}

func TestScaleCluster(t *testing.T) {
	fs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}}
	seedCRD(fs, "Cluster", "db", "pg1")
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/clusters/pg1/scale?ns=db",
		strings.NewReader(`{"instances":5}`)))
	if rec.Code != 200 {
		t.Fatalf("scale status %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := fs.GetCRD(context.Background(), "Cluster", "db", "pg1")
	spec, _, _ := unstructured.NestedInt64(got.Object, "spec", "instances")
	if spec != 5 {
		t.Fatalf("instances not scaled, got %v", spec)
	}
}

func TestScaleClusterInvalid(t *testing.T) {
	fs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}}
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/clusters/pg1/scale?ns=db",
		strings.NewReader(`{"instances":0}`)))
	if rec.Code != 400 {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestEditClusterConfig(t *testing.T) {
	fs := &fakeStore{clusters: []apiv1.Cluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}},
	}}
	seedCRD(fs, "Cluster", "db", "pg1")
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PATCH", "/api/clusters/pg1/config?ns=db",
		strings.NewReader(`{"resources":{"requests":{"cpu":"1"}},"postgresql":{"parameters":{"shared_buffers":"1GB"}}}`)))
	if rec.Code != 200 {
		t.Fatalf("config status %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := fs.GetCRD(context.Background(), "Cluster", "db", "pg1")
	sb, _, _ := unstructured.NestedString(got.Object, "spec", "postgresql", "parameters", "shared_buffers")
	if sb != "1GB" {
		t.Fatalf("postgresql param not set: %s", sb)
	}
}
