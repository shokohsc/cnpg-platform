# CNPG CRD Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add direct management of CNPG Kubernetes CRDs — scaling instances, editing cluster config (resources/storage/image/postgresql.conf), and full CRUD over the other CNPG CRD types — while keeping all SQL-driven browsing intact.

**Architecture:** Add a generic CRD CRUD layer to `kube.Client` operating on `unstructured.Unstructured` against the existing CNPG scheme, expose it through generic REST endpoints (`/api/crds/{kind}`), add two typed convenience endpoints for Cluster scale/config, and build frontend UI (scale stepper + edit panel in Overview; a generic CRD browser) around it.

**Tech Stack:** Go 1.24 (controller-runtime `client.Client`, `unstructured.Unstructured`, `types.MergePatchType`), Vue 3 `<script setup>` + Tailwind, no router (v-if SPA).

**Spec:** `/workspace/docs/superpowers/specs/2026-08-31-cnpg-crd-management-design.md`

## Global Constraints

- CNPG group/version is always `postgresql.cnpg.io/v1`; `apiv1.AddToScheme` (already called in `newScheme`) registers all kinds.
- Valid CRD kinds (whitelist): `Cluster`, `Backup`, `Database`, `DatabaseRole`, `Pooler`, `ScheduledBackup`, `ImageCatalog`, `ClusterImageCatalog`, `Publication`, `Subscription`. All are **namespaced** EXCEPT `ClusterImageCatalog` (cluster-scoped). `DatabaseRoleList` is the list kind of `DatabaseRole` (namespaced, never exposed as an editable kind).
- `ns` query param: for namespaced kinds filters the namespace; omitted → list all namespaces. For cluster-scoped kinds `ns` is ignored.
- Only `Cluster` is edited via typed convenience endpoints (scale/config). No create/delete of `Cluster` CRDs.
- No bespoke per-kind forms — one generic JSON-editor component covers all non-Cluster kinds.
- All existing SQL tabs unchanged.
- Build/test env (Go NOT on PATH): `export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go <cmd> ./...`. Frontend: `npm run build` in `/workspace/frontend` (outputs to `../internal/web/dist`).
- Version floors / no new Go or npm dependencies.

---

### Task 1: Generic CRD CRUD methods on kube.Client

**Files:**
- Modify: `internal/kube/kube.go`
- Test: `internal/kube/kube_test.go`

**Interfaces:**
- Consumes: existing `kube.Client` (`c client.Client`), `newScheme()`.
- Produces: `func CRDGVR(kind string) (schema.GroupVersionResource, error)`; `func CRDNamespaced(kind string) bool`; methods `ListCRD`, `GetCRD`, `CreateCRD`, `UpdateCRD`, `PatchCRD`, `DeleteCRD` (signatures below). Later tasks (web layer, tests) call these.

- [ ] **Step 1: Write the failing kube tests**

Add to `internal/kube/kube_test.go` (imports needed: `"encoding/json"`, `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"`, `"k8s.io/apimachinery/pkg/runtime/schema"`):

```go
func TestCRDWhitelist(t *testing.T) {
	for _, k := range []string{"Cluster", "Backup", "Database", "DatabaseRole", "Pooler",
		"ScheduledBackup", "ImageCatalog", "ClusterImageCatalog", "Publication", "Subscription"} {
		if !CRDNamespaced(k) && k != "ClusterImageCatalog" {
			t.Fatalf("expected %s namespaced", k)
		}
		if CRDNamespaced("ClusterImageCatalog") {
			t.Fatal("ClusterImageCatalog should be cluster-scoped")
		}
	}
	if CRDNamespaced("Bogus") {
		t.Fatal("bogus kind should not be namespaced-aware / should be absent")
	}
}

func TestListCRDRoundTrip(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	for _, name := range []string{"b1", "b2"} {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Backup"})
		obj.SetName(name)
		obj.SetNamespace("db")
		if err := k.CreateCRD(ctx, "Backup", "db", obj); err != nil {
			t.Fatal(err)
		}
	}
	list, err := k.ListCRD(ctx, "Backup", "db")
	if err != nil || len(list) != 2 {
		t.Fatalf("list %d err %v", len(list), err)
	}
	one, err := k.GetCRD(ctx, "Backup", "db", "b1")
	if err != nil || one.GetName() != "b1" {
		t.Fatalf("get err %v", err)
	}
	one2, _ := one.MarshalJSON()
	one2 = append(one2[:len(one2)-1], []byte(`,"spec":{"method":"barmanObjectStore"}}`)...)
	var upd unstructured.Unstructured
	_ = upd.UnmarshalJSON(one2)
	upd.SetResourceVersion(one.GetResourceVersion())
	if err := k.UpdateCRD(ctx, "Backup", "db", &upd); err != nil {
		t.Fatal(err)
	}
	got, _ := k.GetCRD(ctx, "Backup", "db", "b1")
	if got.Object["spec"].(map[string]any)["method"] != "barmanObjectStore" {
		t.Fatal("update did not persist spec")
	}
	if err := k.DeleteCRD(ctx, "Backup", "db", "b1"); err != nil {
		t.Fatal(err)
	}
	after, _ := k.ListCRD(ctx, "Backup", "db")
	if len(after) != 1 {
		t.Fatalf("expected 1 after delete, got %d", len(after))
	}
}

func TestListCRDClusterScoped(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "ClusterImageCatalog"})
	obj.SetName("cat")
	// Default namespaced client: force all-namespaces list just exercises ns "". 
	if err := k.CreateCRD(ctx, "ClusterImageCatalog", "", obj); err != nil {
		t.Fatal(err)
	}
	list, err := k.ListCRD(ctx, "ClusterImageCatalog", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("cluster-scoped list %d err %v", len(list), err)
	}
}

func TestListCRDInvalidKind(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	if _, err := k.ListCRD(context.Background(), "Bogus", "db"); err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestPatchCRD(t *testing.T) {
	c := schemeBuilder()
	k := &Client{c: c}
	ctx := context.Background()
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: "Cluster"})
	obj.SetName("pg")
	obj.SetNamespace("db")
	obj.Object["spec"] = map[string]any{"instances": int64(1), "imageName": "img:17"}
	if err := k.CreateCRD(ctx, "Cluster", "db", obj); err != nil {
		t.Fatal(err)
	}
	if err := k.PatchCRD(ctx, "Cluster", "db", "pg", map[string]any{"spec": map[string]any{"instances": int64(3)}}); err != nil {
		t.Fatal(err)
	}
	got, _ := k.GetCRD(ctx, "Cluster", "db", "pg")
	spec := got.Object["spec"].(map[string]any)
	if spec["instances"] != int64(3) {
		t.Fatalf("patch did not set instances: %v", spec["instances"])
	}
	if spec["imageName"] != "img:17" {
		t.Fatalf("merge patch clobbered sibling field: %v", spec)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go test ./internal/kube/ -run 'TestCRD' -v
```
Expected: FAIL (undefined: CRDNamespaced, ListCRD, etc.).

- [ ] **Step 3: Write minimal implementation**

Add to `internal/kube/kube.go`. New imports: `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"`, `"k8s.io/apimachinery/pkg/runtime/schema"`, `"k8s.io/client-go/util/jsonpath"` (NO — do NOT use jsonpath). Use only:
```go
import (
    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "k8s.io/apimachinery/pkg/runtime/schema"
)
```

Append after `DeleteSecret` (before `ClusterPort`):

```go
const crdGroup = "postgresql.cnpg.io"
const crdVersion = "v1"

// CRD kinds supported by the generic layer, and whether each is cluster-scoped.
var crdScoped = map[string]bool{
	"ClusterImageCatalog": true,
}

func CRDNamespaced(kind string) bool {
	_, ok := crdKinds[kind]
	if !ok {
		return false
	}
	return !crdScoped[kind]
}

var crdKinds = map[string]struct{}{
	"Cluster": {}, "Backup": {}, "Database": {}, "DatabaseRole": {},
	"Pooler": {}, "ScheduledBackup": {}, "ImageCatalog": {},
	"ClusterImageCatalog": {}, "Publication": {}, "Subscription": {},
}

func CRDGVR(kind string) (schema.GroupVersionResource, error) {
	if _, ok := crdKinds[kind]; !ok {
		return schema.GroupVersionResource{}, fmt.Errorf("unsupported CRD kind: %s", kind)
	}
	return schema.GroupVersionResource{Group: crdGroup, Version: crdVersion, Resource: plural(kind)}, nil
}

func plural(kind string) string {
	return strings.ToLower(kind) + "s"
}

// nsFor returns the namespace arg to pass to kube calls (empty for cluster-scoped kinds).
func nsFor(kind, ns string) string {
	if !CRDNamespaced(kind) {
		return ""
	}
	return ns
}

func (k *Client) ListCRD(ctx context.Context, kind, ns string) ([]unstructured.Unstructured, error) {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return nil, err
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind + "List"})
	opts := []client.ListOption{}
	if n := nsFor(kind, ns); n != "" {
		opts = append(opts, client.InNamespace(n))
	}
	if err := k.c.List(ctx, list, opts...); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (k *Client) GetCRD(ctx context.Context, kind, ns, name string) (*unstructured.Unstructured, error) {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if err := k.c.Get(ctx, client.ObjectKey{Namespace: nsFor(kind, ns), Name: name}, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (k *Client) CreateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if n := nsFor(kind, ns); n != "" {
		obj.SetNamespace(n)
	} else {
		obj.SetNamespace("")
	}
	return k.c.Create(ctx, obj)
}

func (k *Client) UpdateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	if n := nsFor(kind, ns); n != "" {
		obj.SetNamespace(n)
	}
	return k.c.Update(ctx, obj)
}

func (k *Client) DeleteCRD(ctx context.Context, kind, ns, name string) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	obj.SetNamespace(nsFor(kind, ns))
	obj.SetName(name)
	err = k.c.Delete(ctx, obj)
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (k *Client) PatchCRD(ctx context.Context, kind, ns, name string, patch map[string]any) error {
	gvr, err := CRDGVR(kind)
	if err != nil {
		return err
	}
	data, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: gvr.Group, Version: gvr.Version, Kind: kind})
	obj.SetName(name)
	opts := []client.PatchOption{}
	if n := nsFor(kind, ns); n != "" {
		opts = append(opts, client.InNamespace(n))
	}
	return k.c.Patch(ctx, obj, client.RawPatch(types.MergePatchType, data), opts...)
}
```

Also add `"encoding/json"` to kube.go imports.

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go test ./internal/kube/ -run 'TestCRD|TestList|TestGet|TestPatch|TestUpsert|TestService|TestCluster' -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/kube/kube.go internal/kube/kube_test.go
git commit -m "feat: generic CRD CRUD methods on kube client"
```

---

### Task 2: Generic CRD REST endpoints + registry helper

**Files:**
- Create: `internal/web/crds.go`
- Modify: `internal/web/server.go` (register routes)
- Test: `internal/web/server_test.go` (fakeStore CRD methods + CRD endpoint tests)

**Interfaces:**
- Consumes: `kube.CRDNamespaced(kind) bool`; `h.cs` (ClusterStore, extended in Task 3); `writeJSON`/`writeErr`/`decode` from json.go; `h.writeError` from server.go.
- Produces: HTTP routes `GET/POST /api/crds/{kind}?ns=`, `GET/PUT/PATCH/DELETE /api/crds/{kind}/{name}?ns=`. Response for list: `[]map[string]any` (each item is the full CRD object). Create returns `{created: name}`. Delete returns `{deleted: name}`. Get/Update return the full CRD object.

- [ ] **Step 1: Add the CRD routes to the mux**

In `internal/web/server.go`, before the `/api/` catch-all and after the existing cluster routes, add:

```go
	mux.HandleFunc("GET /api/crds/{kind}", h.listCRDs)
	mux.HandleFunc("POST /api/crds/{kind}", h.createCRD)
	mux.HandleFunc("GET /api/crds/{kind}/{name}", h.getCRD)
	mux.HandleFunc("PUT /api/crds/{kind}/{name}", h.updateCRD)
	mux.HandleFunc("PATCH /api/crds/{kind}/{name}", h.patchCRD)
	mux.HandleFunc("DELETE /api/crds/{kind}/{name}", h.deleteCRD)
```

Using Go 1.22 `{kind}` path patterns. Note: `{kind}` matches a single segment, so the `{kind}/{name}` routes coexist fine.

- [ ] **Step 2: Write the crds.go handlers**

Create `internal/web/crds.go`:

```go
package web

import (
	"encoding/json"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"cnpg-manager/internal/kube"
)

// validCRDKind rejects unknown kinds (400). ClusterImageCatalog is cluster-scoped
// yet still a valid kind; CRDGVR's whitelist covers it, so this is the single guard.
func validCRDKind(w http.ResponseWriter, kind string) bool {
	if _, err := kube.CRDGVR(kind); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (h *api) listCRDs(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validCRDKind(w, kind) {
		return
	}
	items, err := h.cs.ListCRD(r.Context(), kind, r.URL.Query().Get("ns"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]map[string]any, len(items))
	for i := range items {
		out[i] = items[i].Object
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *api) getCRD(w http.ResponseWriter, r *http.Request) {
	obj, err := h.cs.GetCRD(r.Context(), r.PathValue("kind"), r.URL.Query().Get("ns"), r.PathValue("name"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, obj.Object)
}

type crdCreateReq struct {
	Name   string         `json:"name"`
	Spec   map[string]any `json:"spec"`
}

func (h *api) createCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validCRDKind(w, kind) {
		return
	}
	var in crdCreateReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	obj := &unstructured.Unstructured{}
	obj.Object = map[string]any{"metadata": map[string]any{"name": in.Name}}
	if in.Spec != nil {
		obj.Object["spec"] = in.Spec
	}
	if err := h.cs.CreateCRD(r.Context(), kind, r.URL.Query().Get("ns"), obj); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"created": in.Name})
}

func (h *api) updateCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validCRDKind(w, kind) {
		return
	}
	var body map[string]any
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	obj := &unstructured.Unstructured{Object: body}
	if err := h.cs.UpdateCRD(r.Context(), kind, r.URL.Query().Get("ns"), obj); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": r.PathValue("name")})
}

func (h *api) patchCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validCRDKind(w, kind) {
		return
	}
	var patch map[string]any
	if err := decode(r, &patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), kind, r.URL.Query().Get("ns"), r.PathValue("name"), patch); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"patched": r.PathValue("name")})
}

func (h *api) deleteCRD(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !validCRDKind(w, kind) {
		return
	}
	if err := h.cs.DeleteCRD(r.Context(), kind, r.URL.Query().Get("ns"), r.PathValue("name")); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": r.PathValue("name")})
}
```

Note: `encoding/json` import above is unused (decode handles body). Remove it. The final import block for crds.go is just `"net/http"` + `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"` + `"cnpg-manager/internal/kube"`.

- [ ] **Step 3: Write failing web tests for CRD endpoints**

Add to `internal/web/server_test.go` (add imports: `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"` if used by a helper). Add CRD methods to `fakeStore` (Task 3 does this — but to compile, add minimal CRD methods in this step):

Add to `fakeStore` struct fields: `crds map[string]*unstructured.Unstructured`. Also add methods:

```go
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

// mergeJSON deep-merges patch into base (RFC 7386) using sigs.k8s.io/yaml-free stdlib approach.
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
```

Also add helper to build a fakeStore with a seeded CRD:
```go
func seedCRD(f *fakeStore, kind, ns, name string) {
	cp := &unstructured.Unstructured{}
	cp.SetGroupVersionKind(schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind: kind})
	cp.SetName(name)
	cp.SetNamespace(ns)
	cp.Object["spec"] = map[string]any{"instances": int64(1)}
	_ = f.CreateCRD(context.Background(), kind, ns, cp)
}
```

And tests:

```go
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
	if got["name"] != "b1" {
		t.Fatalf("bad body %v", got)
	}
}

func TestCRDClusterScopedListAllNamespaces(t *testing.T) {
	fs := &fakeStore{}
	seedCRD(fs, "ClusterImageCatalog", "", "cat")
	h := newTestHandler(fs, nil)
	rec := httptest.NewRecorder()
	// ns omitted -> cluster-scoped list
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
```

- [ ] **Step 4: Run tests to verify they fail (compile-error stage acceptable)**

This task's tests reference `h.cs.ListCRD` etc. which need the interface extension from Task 3. To keep the plan sequential, Task 3 extends the interface; run the full web test suite after Task 3.

Run (may fail to compile until Task 3):
```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go test ./internal/web/ -run 'TestCRD' -v
```
Expected: FAIL (undefined method on ClusterStore interface) — proceed to Task 3.

- [ ] **Step 5: Commit**

```bash
git add internal/web/crds.go internal/web/server.go internal/web/server_test.go
git commit -m "feat: generic CRD REST endpoints"
```

---

### Task 3: Extend ClusterStore interface + cluster scale/config endpoints

**Files:**
- Modify: `internal/web/server.go` (interface + import)
- Modify: `internal/web/clusters.go` (scale + config handlers)
- Modify: `internal/web/server_test.go` (already has fake CRD methods from Task 2; add scale/config tests)
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: `kube.CRDGVR`, `kube.CRDNamespaced`, `ClusterStore` methods from Task 2.
- Produces: extended `ClusterStore` interface + routes `PATCH /api/clusters/{cluster}/scale?ns=` (body `{"instances":N}`) and `PATCH /api/clusters/{cluster}/config?ns=` (body: partial spec object merged under `spec`).

- [ ] **Step 1: Extend the ClusterStore interface**

In `internal/web/server.go`, add to the `ClusterStore` interface (after `DeleteSecret`):

```go
	ListCRD(ctx context.Context, kind, ns string) ([]unstructured.Unstructured, error)
	GetCRD(ctx context.Context, kind, ns, name string) (*unstructured.Unstructured, error)
	CreateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error
	UpdateCRD(ctx context.Context, kind, ns string, obj *unstructured.Unstructured) error
	PatchCRD(ctx context.Context, kind, ns, name string, patch map[string]any) error
	DeleteCRD(ctx context.Context, kind, ns, name string) error
```

Add import `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"` to server.go.

- [ ] **Step 2: Register the scale/config routes**

In `internal/web/server.go` mux, after existing cluster routes (before `/api/` catch-all), add:

```go
	mux.HandleFunc("PATCH /api/clusters/{cluster}/scale", h.scaleCluster)
	mux.HandleFunc("PATCH /api/clusters/{cluster}/config", h.editClusterConfig)
```

- [ ] **Step 3: Implement scale + config handlers**

Append to `internal/web/clusters.go`:

```go
type scaleReq struct {
	Instances int `json:"instances"`
}

func (h *api) scaleCluster(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var in scaleReq
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.Instances < 1 {
		writeErr(w, http.StatusBadRequest, "instances must be >= 1")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), "Cluster", cl.Namespace, cl.Name,
		map[string]any{"spec": map[string]any{"instances": in.Instances}}); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": in.Instances})
}

func (h *api) editClusterConfig(w http.ResponseWriter, r *http.Request) {
	cl, err := h.resolveCluster(r.Context(), r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	var spec map[string]any
	if err := decode(r, &spec); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.cs.PatchCRD(r.Context(), "Cluster", cl.Namespace, cl.Name,
		map[string]any{"spec": spec}); err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"updated": cl.Name})
}
```

- [ ] **Step 4: Write failing web tests for scale/config**

Add to `internal/web/server_test.go`:

```go
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
```

Add import `"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"` to server_test.go.

- [ ] **Step 5: Run all web tests + kube tests**

Run:
```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go test ./internal/web/... ./internal/kube/... -v
```
Expected: PASS (all CRD + scale/config tests).

- [ ] **Step 6: Build + vet**

```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go build ./... && /tmp/opencode/go/bin/go vet ./...
```
Expected: exit 0 for both.

- [ ] **Step 7: Commit**

```bash
git add internal/web/server.go internal/web/clusters.go internal/web/server_test.go
git commit -m "feat: cluster scale and config endpoints"
```

---

### Task 4: Widen deployment RBAC

**Files:**
- Modify: `deploy/rbac.yaml`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: ClusterRole granting CRUD over all CNPG CRD types.

- [ ] **Step 1: Widen the ClusterRole rules**

Replace `deploy/rbac.yaml` rules with:

```yaml
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters", "backups", "scheduledbackups", "poolers", "databases", "databaseroles", "imagecatalogs", "publications", "subscriptions"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters", "backups", "scheduledbackups", "poolers", "databases", "databaseroles", "imagecatalogs", "publications", "subscriptions"]
    verbs: ["create", "update", "patch", "delete"]
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusterimagecatalogs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets", "pods"]
    verbs: ["get", "list", "watch", "create", "update"]
```

- [ ] **Step 2: Commit**

```bash
git add deploy/rbac.yaml
git commit -m "chore: widen RBAC for CNPG CRD CRUD"
```

---

### Task 5: Frontend api.ts — Cluster CRD fields + generic crud + scale/config

**Files:**
- Modify: `frontend/src/api.ts`

**Interfaces:**
- Consumes: existing `req<T>(url, init)` helper.
- Produces: `crud` object (`list/get/create/update/patch/delete`), `scale`, `editConfig`; extended `Cluster` interface with `resources`, `storage`, `imageName`, `postgresql`, `instances` (already present). Also exports `KindMeta` constant `CRD_KINDS`.

- [ ] **Step 1: Add the crud helper + Cluster fields**

In `frontend/src/api.ts`:

```ts
export interface ResourceRequirements {
  requests?: { cpu?: string; memory?: string }
  limits?: { cpu?: string; memory?: string }
}
export interface StorageSpec {
  size?: string
  storageClass?: string
}
export interface PostgresConfig {
  parameters?: Record<string, string>
}
```

Extend `Cluster` interface — replace the existing field list by adding after `instances`:
```ts
  resources?: ResourceRequirements
  storage?: StorageSpec
  imageName?: string
  postgresql?: PostgresConfig
```

Add near the bottom of the `api` object:
```ts
  crud: {
    list: (kind: string, ns: string) => req<any[]>(`/api/crds/${kind}?ns=${ns}`),
    get: (kind: string, ns: string, name: string) =>
      req<any>(`/api/crds/${kind}/${encodeURIComponent(name)}?ns=${ns}`),
    create: (kind: string, ns: string, name: string, spec: any) =>
      req<{ created: string }>(`/api/crds/${kind}?ns=${ns}`, { method: 'POST', body: JSON.stringify({ name, spec }) }),
    update: (kind: string, ns: string, name: string, obj: any) =>
      req<{ updated: string }>(`/api/crds/${kind}/${encodeURIComponent(name)}?ns=${ns}`, { method: 'PUT', body: JSON.stringify(obj) }),
    patch: (kind: string, ns: string, name: string, obj: any) =>
      req<{ patched: string }>(`/api/crds/${kind}/${encodeURIComponent(name)}?ns=${ns}`, { method: 'PATCH', body: JSON.stringify(obj) }),
    del: (kind: string, ns: string, name: string) =>
      req<{ deleted: string }>(`/api/crds/${kind}/${encodeURIComponent(name)}?ns=${ns}`, { method: 'DELETE' })
  },
  scale: (c: string, ns: string, instances: number) =>
    req<{ instances: number }>(`/api/clusters/${c}/scale?ns=${ns}`, { method: 'PATCH', body: JSON.stringify({ instances }) }),
  editConfig: (c: string, ns: string, spec: any) =>
    req<{ updated: string }>(`/api/clusters/${c}/config?ns=${ns}`, { method: 'PATCH', body: JSON.stringify(spec) }),
```

Add exported constant:
```ts
export const CRD_KINDS = [
  { kind: 'Backup', namespaced: true },
  { kind: 'Database', namespaced: true },
  { kind: 'DatabaseRole', namespaced: true },
  { kind: 'Pooler', namespaced: true },
  { kind: 'ScheduledBackup', namespaced: true },
  { kind: 'ImageCatalog', namespaced: true },
  { kind: 'ClusterImageCatalog', namespaced: false }
] as const
```

- [ ] **Step 2: Build the frontend**

```bash
cd /workspace/frontend && npm run build
```
Expected: exit 0 (note: `vue-tsc` is not installed; `vite build` transpiles without type-check, so type errors surface via build — verify visually too).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api.ts
git commit -m "feat: frontend CRD crud + scale/config api helpers"
```

---

### Task 6: Overview tab — scale stepper + edit config panel

**Files:**
- Modify: `frontend/src/views/ClusterDetail.vue`
- Test: frontend build

**Interfaces:**
- Consumes: `api.scale`, `api.editConfig`; `Cluster` fields `instances`, `resources`, `storage`, `imageName`.
- Produces: Overview tab UI with a scale stepper and an edit panel.

- [ ] **Step 1: Add scale + edit state and handlers**

In `ClusterDetail.vue` `<script setup>` add:

```ts
import { ref } from 'vue'
import { store } from '../store'

const instances = ref(props.cluster.instances || 1)
const scaling = ref(false)
const edit = ref(false)
const editImage = ref(props.cluster.imageName || '')
const editStorage = ref(props.cluster.storage?.size || '')
const editCpu = ref(props.cluster.resources?.requests?.cpu || '')
const editMem = ref(props.cluster.resources?.requests?.memory || '')
const pgConf = ref(JSON.stringify(props.cluster.postgresql?.parameters || {}, null, 2))
const saving = ref(false)
const actionError = ref('')

async function doScale(delta: number) {
  const next = Math.max(1, instances.value + delta)
  scaling.value = true
  actionError.value = ''
  try {
    await api.scale(props.cluster.name, props.cluster.namespace, next)
    instances.value = next
  } catch (e) { actionError.value = String(e) } finally { scaling.value = false }
}

async function saveConfig() {
  saving.value = true
  actionError.value = ''
  const spec: any = {}
  if (editImage.value) spec.imageName = editImage.value
  if (editStorage.value) spec.storage = { size: editStorage.value }
  if (editCpu.value || editMem.value) {
    spec.resources = { requests: {} as any, limits: {} as any }
    if (editCpu.value) spec.resources.requests.cpu = editCpu.value
    if (editMem.value) spec.resources.requests.memory = editMem.value
  }
  let params: Record<string, string> = {}
  try { params = pgConf.value ? JSON.parse(pgConf.value) : {} } catch { params = {} }
  spec.postgresql = { ...(props.cluster.postgresql || {}), parameters: params }
  try {
    await api.editConfig(props.cluster.name, props.cluster.namespace, spec)
    edit.value = false
    await store.loadClusters()
  } catch (e) { actionError.value = String(e) } finally { saving.value = false }
}
```

- [ ] **Step 2: Add the overview UI**

In the Overview tab (`<div v-else ...>` at the end of the template), add a scale control and an edit panel before/after the stat cards. Replace the block starting `<div v-else class="grid grid-cols-2 md:grid-cols-4 gap-3">` with:

```html
    <div v-else>
      <div class="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
        <div class="bg-panel border border-border rounded p-3">
          <div class="text-xs text-dim">Postgres</div>
          <div class="text-lg font-semibold">v{{ cluster.version }}</div>
        </div>
        <div class="bg-panel border border-border rounded p-3">
          <div class="text-xs text-dim">Instances</div>
          <div class="text-lg font-semibold flex items-center gap-2">
            <button class="px-2 rounded bg-panel2 border border-border" :disabled="scaling || instances <= 1" @click="doScale(-1)">−</button>
            {{ instances }}
            <button class="px-2 rounded bg-panel2 border border-border" :disabled="scaling" @click="doScale(1)">+</button>
          </div>
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

      <div v-if="actionError" class="text-red-400 text-sm mb-2">{{ actionError }}</div>

      <div class="bg-panel border border-border rounded p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="font-medium">Cluster config</h2>
          <button v-if="!edit" class="px-3 py-1 rounded bg-accent text-bg text-sm" @click="edit = true">Edit</button>
        </div>
        <div v-if="!edit" class="grid grid-cols-2 gap-3 text-sm">
          <div><span class="text-dim">Image</span> {{ cluster.imageName || '—' }}</div>
          <div><span class="text-dim">Storage</span> {{ cluster.storage?.size || '—' }}</div>
          <div><span class="text-dim">CPU req</span> {{ cluster.resources?.requests?.cpu || '—' }}</div>
          <div><span class="text-dim">Mem req</span> {{ cluster.resources?.requests?.memory || '—' }}</div>
        </div>
        <div v-else class="grid grid-cols-2 gap-3">
          <label class="text-sm">Image<input v-model="editImage" class="w-full bg-panel2 border border-border rounded px-2 py-1" /></label>
          <label class="text-sm">Storage size<input v-model="editStorage" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="1Gi" /></label>
          <label class="text-sm">CPU request<input v-model="editCpu" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="500m" /></label>
          <label class="text-sm">Memory request<input v-model="editMem" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="1Gi" /></label>
          <label class="text-sm col-span-2">postgresql.parameters (JSON)<textarea v-model="pgConf" rows="4" class="w-full bg-panel2 border border-border rounded px-2 py-1 font-mono text-xs"></textarea></label>
          <div class="col-span-2 flex gap-2">
            <button class="px-3 py-1 rounded bg-accent text-bg text-sm" :disabled="saving" @click="saveConfig">{{ saving ? 'Saving…' : 'Save' }}</button>
            <button class="px-3 py-1 rounded border border-border text-sm" @click="edit = false">Cancel</button>
          </div>
        </div>
      </div>
    </div>
```

- [ ] **Step 3: Build**

```bash
cd /workspace/frontend && npm run build
```
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/ClusterDetail.vue
git commit -m "feat: overview scale stepper and config edit panel"
```

---

### Task 7: Generic CRD browser (shared JSON editor component)

**Files:**
- Create: `frontend/src/components/JsonEditor.vue`
- Create: `frontend/src/views/CrdBrowser.vue`
- Modify: `frontend/src/App.vue` (sidebar nav + main view switch)
- Test: frontend build

**Interfaces:**
- Consumes: `api.crud`, `CRD_KINDS` from api.ts.
- Produces: a `CrdBrowser` view with kind selection, list, detail/edit (JsonEditor), create form, delete.

- [ ] **Step 1: Write the shared JsonEditor component**

Create `frontend/src/components/JsonEditor.vue`:

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{ modelValue: any }>()
const emit = defineEmits(['update:modelValue', 'error'])
const text = ref(JSON.stringify(props.modelValue ?? {}, null, 2))

watch(() => props.modelValue, (v) => { text.value = JSON.stringify(v ?? {}, null, 2) })

function update() {
  try {
    const parsed = JSON.parse(text.value)
    emit('update:modelValue', parsed)
    emit('error', '')
  } catch (e) {
    emit('error', String(e))
  }
}
</script>

<template>
  <textarea :value="text" rows="16" spellcheck="false"
    class="w-full bg-panel2 border border-border rounded px-2 py-1 font-mono text-xs"
    @input="text = ($event.target as HTMLTextAreaElement).value"
    @change="update"></textarea>
</template>
```

- [ ] **Step 2: Write the CrdBrowser view**

Create `frontend/src/views/CrdBrowser.vue`:

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, CRD_KINDS } from '../api'
import JsonEditor from '../components/JsonEditor.vue'

const selected = ref<typeof CRD_KINDS[number] | null>(null)
const ns = ref('')
const items = ref<any[]>([])
const current = ref<any | null>(null)
const error = ref('')
const created = ref(false)
const newName = ref('')
const newSpec = ref<any>({})

async function loadList() {
  if (!selected.value) return
  error.value = ''
  try {
    items.value = await api.crud.list(selected.value.kind, selected.value.namespaced ? ns.value : '')
    current.value = null
  } catch (e) { error.value = String(e) }
}
watch(() => selected.value, loadList)

async function open(item: any) {
  current.value = await api.crud.get(selected.value!.kind,
    selected.value!.namespaced ? (item.metadata?.namespace || '') : '', item.metadata?.name)
  created.value = false
}

async function saveEdit() {
  error.value = ''
  try {
    await api.crud.update(selected.value!.kind,
      selected.value!.namespaced ? (current.value.metadata?.namespace || '') : '',
      current.value.metadata?.name, current.value)
    await loadList()
  } catch (e) { error.value = String(e) }
}

async function remove() {
  error.value = ''
  try {
    await api.crud.del(selected.value!.kind,
      selected.value!.namespaced ? (current.value.metadata?.namespace || '') : '',
      current.value.metadata?.name)
    current.value = null
    await loadList()
  } catch (e) { error.value = String(e) }
}

async function create() {
  error.value = ''
  try {
    await api.crud.create(selected.value!.kind, selected.value!.namespaced ? ns.value : '', newName.value, newSpec.value)
    newName.value = ''
    newSpec.value = {}
    created.value = false
    await loadList()
  } catch (e) { error.value = String(e) }
}

const phaseOf = (item: any) => item.status?.phase || item.status?.phase?.phase || 'pending'
</script>

<template>
  <div class="grid grid-cols-4 gap-6">
    <div class="col-span-1">
      <h2 class="font-medium mb-3">CRDs</h2>
      <nav class="space-y-1">
        <button v-for="k in CRD_KINDS" :key="k.kind"
          class="w-full text-left px-3 py-1.5 rounded text-sm hover:bg-panel2"
          :class="selected?.kind === k.kind ? 'bg-panel2 text-accent' : 'text-fg'"
          @click="selected = k">
          {{ k.kind }}<span v-if="k.namespaced" class="text-dim text-xs"> (ns)</span>
        </button>
      </nav>
      <label v-if="selected?.namespaced" class="block mt-4 text-sm">
        Namespace
        <input v-model="ns" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="all namespaces" @change="loadList" />
      </label>
    </div>

    <div class="col-span-1">
      <div class="flex items-center justify-between mb-2">
        <h3 class="font-medium">{{ selected?.kind || '—' }}</h3>
        <button v-if="selected" class="px-2 py-1 rounded bg-accent text-bg text-xs" @click="created = !created">+ New</button>
      </div>
      <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
      <ul class="space-y-1">
        <li v-for="it in items" :key="it.metadata?.name">
          <button class="w-full text-left px-2 py-1 rounded text-sm hover:bg-panel2"
            @click="open(it)">
            <span class="font-mono">{{ it.metadata?.name }}</span>
            <span v-if="it.metadata?.namespace" class="text-dim text-xs ml-1">{{ it.metadata.namespace }}</span>
          </button>
        </li>
        <li v-if="!items.length" class="text-dim text-sm">No {{ selected?.kind }} found.</li>
      </ul>
    </div>

    <div class="col-span-2">
      <template v-if="created && selected">
        <h3 class="font-medium mb-2">Create {{ selected.kind }}</h3>
        <label class="block text-sm mb-2">Name<input v-model="newName" class="w-full bg-panel2 border border-border rounded px-2 py-1" /></label>
        <JsonEditor v-model="newSpec" @error="error = $event" />
        <div class="mt-2"><button class="px-3 py-1 rounded bg-accent text-bg text-sm" @click="create">Create</button></div>
      </template>
      <template v-else-if="current && selected">
        <div class="flex items-center justify-between mb-2">
          <h3 class="font-medium font-mono">{{ current.metadata?.name }}</h3>
          <div class="flex gap-2">
            <button class="px-2 py-1 rounded bg-accent text-bg text-xs" @click="saveEdit">Save</button>
            <button class="px-2 py-1 rounded border border-red-400 text-red-400 text-xs" @click="remove">Delete</button>
          </div>
        </div>
        <JsonEditor v-model="current" @error="error = $event" />
      </template>
      <div v-else class="text-dim text-sm">Select a kind and an item to edit, or create a new one.</div>
    </div>
  </div>
</template>
```

- [ ] **Step 3: Wire CrdBrowser into App.vue navigation**

Modify `frontend/src/App.vue`:
- Add import: `import CrdBrowser from './views/CrdBrowser.vue'`
- Add a `showCrds` ref: `const showCrds = ref(false)`
- When selecting a cluster, hide CRD browser: change `store.selectCluster` usage. Simplest: in the template's All-clusters button and cluster buttons add `@click="showCrds = false"`; add a "CRDs" button in the sidebar footer that sets `showCrds = true` and clears selection.
- Modify `<main>` to:

```html
    <main class="flex-1 overflow-y-auto">
      <ConnectModal v-if="store.connect.open" />
      <CrdBrowser v-if="showCrds" />
      <Clusters v-else-if="!store.current" />
      <ClusterDetail v-else :cluster="store.current" />
    </main>
```

Add import of `ref`. The sidebar footer becomes:

```html
      <div class="px-4 py-3 text-xs text-dim space-y-1">
        <button class="text-accent hover:underline" @click="showCrds = false; store.selectCluster(null)">All clusters</button>
        <div><button class="text-accent hover:underline" @click="showCrds = true; store.selectCluster(null)">CRD browser</button></div>
      </div>
```

- [ ] **Step 4: Build**

```bash
cd /workspace/frontend && npm run build
```
Expected: exit 0.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/JsonEditor.vue frontend/src/views/CrdBrowser.vue frontend/src/App.vue
git commit -m "feat: generic CRD browser in UI"
```

---

### Task 8: Full verification

**Files:**
- None (verification only).

**Interfaces:**
- None.

- [ ] **Step 1: Full Go test + build + vet**

```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go build ./... && /tmp/opencode/go/bin/go vet ./... && /tmp/opencode/go/bin/go test ./...
```
Expected: all exit 0, all tests pass (note: `internal/pg` integration tests may be skipped if no live DB).

- [ ] **Step 2: Full frontend build**

```bash
cd /workspace/frontend && npm run build
```
Expected: exit 0.

- [ ] **Step 3: Manual API smoke-test against fake-backed server**

Run a quick temporary check that the CRD routes register and respond (using existing tests as the authority — the `TestCRD*` and scale/config tests already cover this). If desired, start the binary against no cluster to confirm it boots:
```bash
export GOCACHE=/tmp/opencode/gocache GOPATH=/tmp/opencode/gopath GOMODCACHE=/tmp/opencode/gomodcache CGO_ENABLED=0 && /tmp/opencode/go/bin/go run ./cmd/server
```
Expected: logs start; ctrl-C to stop. (Skip if no kubeconfig — boot may rely on cluster access.)

- [ ] **Step 4: Commit any leftover + report**

```bash
git add -A && git commit -m "chore: verification pass" || true
```

---

## Self-Review Notes

- **Spec coverage:** All spec sections mapped — generic CRD layer (Task 1), generic REST endpoints (Task 2), interface extension (Task 3), cluster scale/config (Task 3), frontend api helpers (Task 5), overview scale+edit (Task 6), CRD browser + generic JSON editor (Task 7), RBAC (Task 4), tests (Tasks 1–3), verification (Task 8). Out-of-scope (cluster create/delete, bespoke forms, SQL tab changes) intentionally excluded.
- **Placeholders:** none — every step has concrete code.
- **Type consistency:** `crud.del`/`create`/`update`/`patch`/`list`/`get` names match between api.ts and CrdBrowser.vue; `api.scale`/`api.editConfig` match ClusterDetail.vue; kube method signatures match web handlers and fakeStore.
- Build note: `vue-tsc` is not installed (package.json has only `vite build`), so frontend type-checking is via `vite build`; noted in the plan.
