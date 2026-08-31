# Replace DB/Role SQL with CNPG CRDs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move database create/drop and role create/drop from raw PostgreSQL SQL onto CloudNativePG Kubernetes resources (the `Database` CRD and `cluster.spec.managed.roles`) so write operations stop issuing SQL against the cluster.

**Architecture:** Write operations for databases use the namespaced standalone `Database` CRD (group `postgresql.cnpg.io/v1`, kind `Database`). Write operations for roles mutate the typed `Cluster.Spec.Managed.Roles []RoleConfiguration` on the cluster object and issue a controller-runtime `Update`. Read/metadata paths (ListDatabases, ListRoles, RolePassword, table metadata, RunSQL) deliberately remain SQL because CRDs cannot report database size, system databases, or role memberships. Roles' per-database `GRANT` is dropped (CNPG managed roles cannot express it).

**Tech Stack:** Go 1.24, controller-runtime `client.Client`, k8s unstructured dynamic CRD layer, CNPG `apiv1` (v1.25.0 pinned), Vue 3 + TypeScript frontend.

**Spec/Design:** See the design presented and approved 2026-08-31 (Databases → `Database` CRD with `databaseReclaimPolicy: delete` and CR deletion; Roles → `cluster.spec.managed.roles`; grantDB dropped; reads stay SQL).

## Global Constraints

- Compile-time guard `var _ ClusterStore = (*kube.Client)(nil)` and `var _ PG = (*pg.Server)(nil)` in `internal/web/server.go` MUST keep passing (adjust interfaces to match real method sets).
- Use CNPG `apiv1` typed structs wherever possible; route through existing unstructured generic CRD layer (`ListCRD`/`CreateCRD`/`DeleteCRD`) for the `Database` kind because that is how `internal/kube` already works for CRD kinds.
- Do NOT add comments to code. `ponytail:` comments are allowed only where a real ceiling is deliberately cut.
- No lint/format tooling configured; follow existing style.

---

### Task 1: kube layer — Database CRD helpers

**Files:**
- Modify: `internal/kube/kube.go` (extend `Client`)
- Test: `internal/kube/kube_test.go`

**Interfaces:**
- Consumes: existing `Client.c` (`controller-runtime client.Client`), `CRDGVR`, `CreateCRD`, `DeleteCRD`, `apiv1.Database`, `apiv1.Cluster`.
- Produces:
  - `func (k *Client) CreateDatabase(ctx context.Context, cl *apiv1.Cluster, name, owner, template, encoding string) error`
  - `func (k *Client) DeleteDatabase(ctx context.Context, cl *apiv1.Cluster, name string) error`

Design: `CreateDatabase` builds an `*unstructured.Unstructured` of kind `Database` with namespace = `cl.Namespace`, name = the database name, `spec.cluster.name = cl.Name`, `spec.name = name`, and the given `spec.owner`/`spec.template`/`spec.encoding` (omitempty), plus `spec.databaseReclaimPolicy = "delete"` so a later CR deletion drops the database in PG. `DeleteDatabase` calls `DeleteCRD("Database", cl.Namespace, name)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/kube/kube_test.go` a test using a fake controller-runtime client (check how `kube_test.go` currently constructs a `Client` first; reuse its scheme/setup pattern). If the test file currently only tests pure helpers (no client), construct a `Client` via `client.New(fake.NewClientBuilder().WithScheme(newScheme()).Build())` — note `newScheme()` is unexported in package `kube`, so the test (same package, `package kube`) can call it.

```go
func TestCreateAndDeleteDatabase(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}}
	c, err := client.New(fake.NewClientBuilder().WithScheme(newScheme()).Build(), client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	k := &Client{c: c}
	if err := k.CreateDatabase(context.Background(), cl, "app", "owner1", "template0", ""); err != nil {
		t.Fatal(err)
	}
	got, err := k.GetCRD(context.Background(), "Database", "db", "app")
	if err != nil {
		t.Fatal(err)
	}
	spec, _, _ := unstructured.NestedString(got.Object, "spec", "name")
	if spec != "app" {
		t.Fatalf("spec.name = %q", spec)
	}
	if p, _, _ := unstructured.NestedString(got.Object, "spec", "databaseReclaimPolicy"); p != "delete" {
		t.Fatalf("reclaim policy = %q, want delete", p)
	}
	if err := k.DeleteDatabase(context.Background(), cl, "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.GetCRD(context.Background(), "Database", "db", "app"); err == nil {
		t.Fatal("expected not found after delete")
	}
}
```

Ensure the fake client setup compiles: add imports `sigs.k8s.io/controller-runtime/pkg/client/fake` and `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured` if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/kube/ -run TestCreateAndDeleteDatabase -v`
Expected: FAIL with "undefined: k.CreateDatabase" (method not yet defined).

- [ ] **Step 3: Write minimal implementation**

In `internal/kube/kube.go`, add:

```go
func (k *Client) CreateDatabase(ctx context.Context, cl *apiv1.Cluster, name, owner, template, encoding string) error {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name},
		"spec": map[string]any{
			"cluster":               map[string]any{"name": cl.Name},
			"name":                  name,
			"databaseReclaimPolicy": "delete",
		},
	}}
	spec := obj.Object["spec"].(map[string]any)
	if owner != "" {
		spec["owner"] = owner
	}
	if template != "" {
		spec["template"] = template
	}
	if encoding != "" {
		spec["encoding"] = encoding
	}
	return k.CreateCRD(ctx, "Database", cl.Namespace, obj)
}

func (k *Client) DeleteDatabase(ctx context.Context, cl *apiv1.Cluster, name string) error {
	return k.DeleteCRD(ctx, "Database", cl.Namespace, name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/kube/ -run TestCreateAndDeleteDatabase -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/kube/kube.go internal/kube/kube_test.go
git commit -m "feat(kube): create/delete databases via Database CRD"
```

---

### Task 2: kube layer — managed roles helpers

**Files:**
- Modify: `internal/kube/kube.go` (extend `Client`)
- Test: `internal/kube/kube_test.go`

**Interfaces:**
- Consumes: `Client.GetCluster`, `Client.c.Update`, `apiv1.Cluster`, `apiv1.RoleConfiguration`, `corev1.LocalObjectReference`, `apiv1.EnsureOption` (values `apiv1.EnsurePresent`, `apiv1.EnsureAbsent` — verify exact constant names in `base_types.go`).
- Produces:
  - `func (k *Client) CreateManagedRole(ctx context.Context, cl *apiv1.Cluster, name, secretName string, super, createDB bool) error`
  - `func (k *Client) DropManagedRole(ctx context.Context, cl *apiv1.Cluster, name string) error`

Design: `CreateManagedRole` fetches the current cluster (to avoid clobbering concurrent role changes) via `GetCluster`, appends a `RoleConfiguration{Name: name, Login: true, Superuser: super, CreateDB: createDB, PasswordSecret: &corev1.LocalObjectReference{Name: secretName}}` unless one with the same `Name` already exists, then `k.c.Update(ctx, cl)`. `DropManagedRole` fetches, removes the entry whose `Name == name`, then `k.c.Update(ctx, cl)`.

Note: the fresh cluster read MUST be done with `k.GetCluster` (typed) so `Spec.Managed.Roles` is populated; do not mutate the caller's passed-in `cl` pointer without refetching, because web passes a snapshot from `resolveCluster`.

Verify the `EnsureOption` constant names in `/home/opencode/go/pkg/mod/github.com/cloudnative-pg/cloudnative-pg@v1.25.0/api/v1/base_types.go` (likely `EnsurePresent`/`EnsureAbsent`). If absent, use literal strings `"present"`/`"absent"` matching the kubebuilder enum.

- [ ] **Step 1: Write the failing test**

```go
func TestCreateAndDropManagedRole(t *testing.T) {
	cl := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "db"}}
	c, err := client.New(fake.NewClientBuilder().WithScheme(newScheme()).Build(), client.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Create(context.Background(), cl); err != nil {
		t.Fatal(err)
	}
	k := &Client{c: c}
	if err := k.CreateManagedRole(context.Background(), cl, "app", "pg1-app", false, true); err != nil {
		t.Fatal(err)
	}
	got, _ := k.GetCluster(context.Background(), "db", "pg1")
	if got.Spec.Managed == nil || len(got.Spec.Managed.Roles) != 1 {
		t.Fatalf("managed roles = %+v", got.Spec.Managed)
	}
	r := got.Spec.Managed.Roles[0]
	if r.Name != "app" || !r.Login || !r.CreateDB || r.Superuser {
		t.Fatalf("bad role config %+v", r)
	}
	if r.PasswordSecret == nil || r.PasswordSecret.Name != "pg1-app" {
		t.Fatalf("passwordSecret = %+v", r.PasswordSecret)
	}
	if err := k.DropManagedRole(context.Background(), cl, "app"); err != nil {
		t.Fatal(err)
	}
	got2, _ := k.GetCluster(context.Background(), "db", "pg1")
	if got2.Spec.Managed == nil || len(got2.Spec.Managed.Roles) != 0 {
		t.Fatalf("roles after drop = %+v", got2.Spec.Managed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/kube/ -run TestCreateAndDropManagedRole -v`
Expected: FAIL with "undefined: k.CreateManagedRole".

- [ ] **Step 3: Write minimal implementation**

```go
func (k *Client) CreateManagedRole(ctx context.Context, cl *apiv1.Cluster, name, secretName string, super, createDB bool) error {
	cur, err := k.GetCluster(ctx, cl.Namespace, cl.Name)
	if err != nil {
		return err
	}
	roles := &cur.Spec
	if cur.Spec.Managed == nil {
		cur.Spec.Managed = &apiv1.ManagedConfiguration{}
	}
	for _, r := range cur.Spec.Managed.Roles {
		if r.Name == name {
			return nil
		}
	}
	cur.Spec.Managed.Roles = append(cur.Spec.Managed.Roles, apiv1.RoleConfiguration{
		Name:           name,
		Login:          true,
		Superuser:      super,
		CreateDB:       createDB,
		PasswordSecret: &corev1.LocalObjectReference{Name: secretName},
	})
	return k.c.Update(ctx, cur)
}

func (k *Client) DropManagedRole(ctx context.Context, cl *apiv1.Cluster, name string) error {
	cur, err := k.GetCluster(ctx, cl.Namespace, cl.Name)
	if err != nil {
		return err
	}
	if cur.Spec.Managed == nil {
		return nil
	}
	kept := cur.Spec.Managed.Roles[:0]
	for _, r := range cur.Spec.Managed.Roles {
		if r.Name != name {
			kept = append(kept, r)
		}
	}
	cur.Spec.Managed.Roles = kept
	return k.c.Update(ctx, cur)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/kube/ -run TestCreateAndDropManagedRole -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/kube/kube.go internal/kube/kube_test.go
git commit -m "feat(kube): manage roles via cluster.spec.managed.roles"
```

---

### Task 3: web layer — widen ClusterStore interface and switch DB handlers

**Files:**
- Modify: `internal/web/server.go` (interface `ClusterStore`, `PG` interface)
- Modify: `internal/web/databases.go` (handlers)
- Modify: `internal/web/server_test.go` (fakeStore + fakePG + db tests)
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: Task 1 (`CreateDatabase`/`DeleteDatabase`), Task 2 (`CreateManagedRole`/`DropManagedRole`) on `kube.Client`.
- Produces: updated `ClusterStore` interface including the four new methods; updated `PG` interface with `CreateDatabase`/`DropDatabase`/`CreateRole`/`DropRole` REMOVED.

Plan:
1. In `internal/web/server.go`, add to `ClusterStore`:
```go
	CreateDatabase(ctx context.Context, cl *apiv1.Cluster, name, owner, template, encoding string) error
	DeleteDatabase(ctx context.Context, cl *apiv1.Cluster, name string) error
	CreateManagedRole(ctx context.Context, cl *apiv1.Cluster, name, secretName string, super, createDB bool) error
	DropManagedRole(ctx context.Context, cl *apiv1.Cluster, name string) error
```
2. In `internal/web/server.go`, remove from `PG` interface:
```go
	CreateDatabase(ctx context.Context, name, owner, template, encoding string) error
	DropDatabase(ctx context.Context, name string) error
	CreateRole(ctx context.Context, name, password string, opts pg.CreateRoleOptions) error
	DropRole(ctx context.Context, name string) error
```
3. Rewrite `internal/web/databases.go` handlers to use `h.cs` instead of `h.connectPG`:
   - `createDatabase`: keep system-DB guard (`pg.IsSystemDB`), drop the `connectPG`/`Close`, call `h.cs.CreateDatabase(ctx, cl, body.Name, body.Owner, body.Template, body.Encoding)`.
   - `dropDatabase`: drop the `connectPG`/`Close`, call `h.cs.DeleteDatabase(ctx, cl, r.PathValue("db"))`.
4. In `internal/web/server_test.go`:
   - `fakeStore`: add fields to record db/role operations and implement the four new methods (append to slices, no cluster read needed). To satisfy `DropManagedRole`'s refetch requirement in tests, return a stored cluster; simplest: add `managedRoles []apiv1.RoleConfiguration` + store the cluster in `GetCluster` so tests can assert. Implement:
```go
func (f *fakeStore) CreateDatabase(ctx context.Context, cl *apiv1.Cluster, name, owner, template, encoding string) error {
	f.dbCreated = append(f.dbCreated, name)
	return nil
}
func (f *fakeStore) DeleteDatabase(ctx context.Context, cl *apiv1.Cluster, name string) error {
	f.dbDropped = append(f.dbDropped, name)
	return nil
}
func (f *fakeStore) CreateManagedRole(ctx context.Context, cl *apiv1.Cluster, name, secretName string, super, createDB bool) error {
	f.rolesCreated = append(f.rolesCreated, name)
	return nil
}
func (f *fakeStore) DropManagedRole(ctx context.Context, cl *apiv1.Cluster, name string) error {
	f.rolesDropped = append(f.rolesDropped, name)
	return nil
}
```
   - `fakePG`: remove the now-unneeded `CreateDatabase`/`DropDatabase`/`CreateRole`/`DropRole` methods (they'd be redundant since not in `PG` interface anymore; leaving them is harmless but dead — remove for cleanliness).
   - Rewrite `TestCreateDatabaseValidation`/`TestDropDatabase`/`TestCreateRoleReturnsPasswordAndSecret` to use the CS methods / new responses instead of `pgc.*`.

- [ ] **Step 1: Update the `ClusterStore` interface**

Edit `internal/web/server.go` to add the 4 methods to `ClusterStore` (see above). This will break `fakeStore` (compile error) — that is the failing-test signal.

- [ ] **Step 2: Update the `PG` interface**

Edit `internal/web/server.go` to remove the 4 write methods from `PG`. This breaks `fakePG` if it still implements them? No — removing methods from an interface doesn't break the fake; the fake merely has extra methods. But removing the web handlers' calls to `p.CreateDatabase` etc. will break until handlers are rewritten. Proceed in order.

- [ ] **Step 3: Update `fakeStore` and `fakePG` in the test file**

Implement the 4 new `fakeStore` methods; add tracking fields (`dbCreated`, `dbDropped`, `rolesCreated`, `rolesDropped []string`). Remove `CreateDatabase`/`DropDatabase`/`CreateRole`/`DropRole` from `fakePG`.

- [ ] **Step 4: Rewrite `internal/web/databases.go`**

Apply the handler rewrites described above.

- [ ] **Step 5: Rewrite the database/role tests**

Rewrite `TestDropDatabase` to assert on `cs.dbDropped` (use `newTestHandler(cs, nil)` with no PG). Add a `TestCreateDatabaseOK` asserting `cs.dbCreated` contains the name when a valid non-system name is posted. Rewrite `TestCreateRoleReturnsPasswordAndSecret` to assert `cs.rolesCreated` contains "app" and the response still returns a generated password.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/web/ -v`
Expected: PASS (all web tests).

- [ ] **Step 7: Commit**

```bash
git add internal/web/server.go internal/web/databases.go internal/web/server_test.go
git commit -m "refactor(web): use CNPG CRDs for db/role writes"
```

---

### Task 4: web layer — switch role handlers to managed roles

**Files:**
- Modify: `internal/web/roles.go` (handlers, drop GrantDB)
- Test: `internal/web/server_test.go`

**Interfaces:**
- Consumes: `h.cs.CreateManagedRole`/`h.cs.DropManagedRole`, `kube.RoleSecret`, `kube.UpsertSecret`/`DeleteSecret` (already on `ClusterStore`).
- Produces: rewritten `createRole`/`dropRole` handlers that no longer touch `PG` (no `connectPG`), and no longer accept/use `grantDB`.

Design: `createRole` reads body `{name,password,super,createDB}` (grantDB removed), generates a password if blank, rejects `name == "superuser"`, upserts the password secret `{username,password}` at `kube.RoleSecret(cl, name)` FIRST, then calls `h.cs.CreateManagedRole(ctx, cl, name, secretName, super, createDB)`. On managed-role failure, roll back by deleting the secret. Then return `{name, password}`. `dropRole` calls `h.cs.DropManagedRole(ctx, cl, name)` then `h.cs.DeleteSecret(...)`.

- [ ] **Step 1: Write the failing tests**

In `internal/web/server_test.go`, ensure tests exist asserting: (a) a created role stores a secret and records the role in `cs.rolesCreated`, and (b) dropping a role records it in `cs.rolesDropped` and deletes the secret. Update `TestCreateRoleReturnsPasswordAndSecret` and add `TestDropRole`.

- [ ] **Step 2: Run tests to verify current behavior**

Run: `go test ./internal/web/ -run 'TestCreateRole|TestDropRole' -v`
Expected: (already rewritten in Task 3) — if flaky, ensure assertions match the new store calls.

- [ ] **Step 3: Rewrite `internal/web/roles.go`**

Apply the handler rewrites above. Remove `grantDB` from the request body struct. Keep `randomPassword`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/roles.go internal/web/server_test.go
git commit -m "refactor(web): manage roles via cluster spec"
```

---

### Task 5: remove dead SQL write methods from pg package

**Files:**
- Modify: `internal/pg/databases.go` (remove `createDatabaseSQL`, `CreateDatabase`, `DropDatabase`)
- Modify: `internal/pg/roles.go` (remove `createRoleSQL`, `CreateRole`, `DropRole`; remove `CreateRoleOptions`)
- Modify: `internal/pg/pg_test.go` (remove `TestCreateDatabaseSQL`, `TestCreateRoleSQL`)
- Modify: `internal/pg/integration_test.go` (remove write-DB/role setup that used the removed methods)
- Test: `internal/pg/pg_test.go`, `internal/pg/integration_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: a `pg` package with no database/role write methods. `ListDatabases`, `ListRoles`, `RolePassword`, `RunSQL`, tables methods remain.

- [ ] **Step 1: Remove dead methods from `internal/pg/databases.go`**

Delete `createDatabaseSQL`, `(*Server).CreateDatabase`, `(*Server).DropDatabase`. Keep `ListDatabases`, `IsSystemDB` usage (system-DB guard stays in web handler).

- [ ] **Step 2: Remove dead methods from `internal/pg/roles.go`**

Delete `createRoleSQL`, `(*Server).CreateRole`, `(*Server).DropRole`, and the `CreateRoleOptions` type. Keep `ListRoles`, `RolePassword`.

- [ ] **Step 3: Remove dead tests from `internal/pg/pg_test.go`**

Delete `TestCreateDatabaseSQL` and `TestCreateRoleSQL` (lines ~33 and ~38-42 that assert the removed builders).

- [ ] **Step 4: Remove dead integration-test setup**

In `internal/pg/integration_test.go`, remove the `CreateDatabase`/`DropDatabase`/`CreateRole`/`DropRole` fixture setup lines (e.g. lines 33-34, 50, 69-70, 86, 105-106, 143). Keep the SQL/tables/roles read tests.

- [ ] **Step 5: Run tests to verify compile + pass**

Run: `go vet ./internal/pg/ && go test ./internal/pg/ -v`
Expected: vet clean, tests PASS. The integration test may be skipped without a live Postgres (guard with build tag / env as before — confirm it skips, don't make it fail).

- [ ] **Step 6: Commit**

```bash
git add internal/pg/databases.go internal/pg/roles.go internal/pg/pg_test.go internal/pg/integration_test.go
git commit -m "refactor(pg): remove SQL db/role write path"
```

---

### Task 6: frontend — remove grantDB field

**Files:**
- Modify: `frontend/src/views/RolesTab.vue`

**Interfaces:**
- Consumes: `api.createRole(cluster, ns, form)` (accepts `object` — no type change needed).
- Produces: role-create form without the `grantDB` input.

- [ ] **Step 1: Remove `grantDB` from the form object**

In `RolesTab.vue` lines 11 and 24, change `{ name: '', password: '', grantDB: '', super: false, createDB: false }` to `{ name: '', password: '', super: false, createDB: false }`.

- [ ] **Step 2: Remove the grantDB input**

Delete line 48 (the `v-model="form.grantDB"` input).

- [ ] **Step 3: Verify frontend builds**

Run: `npm --prefix /workspace/frontend run build` (or `cd frontend && npm run build`).
Expected: build succeeds (no TS reference to `grantDB`).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/views/RolesTab.vue
git commit -m "refactor(ui): drop per-database grant from role form"
```

---

### Task 7: final verification

**Files:**
- None (verification only).

- [ ] **Step 1: Run the full test suite + vet**

Run: `go vet ./... && go test ./internal/...`
Expected: clean and passing.

- [ ] **Step 2: Verify no remaining DB/role write SQL in the pg package**

Run: `grep -rn "CREATE DATABASE\|DROP DATABASE\|CREATE ROLE\|DROP ROLE\|GRANT ALL" internal/pg/`
Expected: no matches (read queries may mention `pg_database`/`pg_roles` but no write verbs).

- [ ] **Step 3: Build the binary**

Run: `make build` (requires frontend build already done in Task 6) — if `make frontend` not yet run, run `make frontend && make build`.
Expected: produces `bin/cnpg-manager`.

- [ ] **Step 4: Confirm compile-time guards still hold**

`go build ./...` should succeed (guards in `server.go` are compile-time; a build failure would indicate interface mismatch).

- [ ] **Step 5: Report completion**

Summarize: databases and roles now use CNPG CRDs; reads/list/table/RunSQL stay SQL by design; grantDB removed.
