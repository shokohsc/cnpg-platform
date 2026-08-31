# Design: CNPG CRD Management (edit / scale / other CRD types)

Date: 2026-08-31
Status: Draft (awaiting user review)

## Purpose

The existing app (see `2026-08-31-cnpg-manager-design.md`) manages CNPG
clusters primarily by connecting to live Postgres and running SQL. This
feature adds the ability to **manage CNPG Kubernetes CRDs directly** —
scaling instances, editing cluster config (resources, storage, image,
postgresql.conf), and full CRUD over other CNPG CRD types — while **keeping
all current SQL-driven browsing intact**.

Scope decisions confirmed with the user:
- **Add** the new capabilities: edit cluster config, scale instances, and
  view/edit other CNPG CRD types.
- **Do NOT add** create/delete of `Cluster` CRDs.
- **Full CRUD** (create/read/update/delete) for the other CRD types.
- **Keep** Databases, Roles, SQL, Tables, Backups tabs unchanged.

## Approach (selected: generic CRD layer)

The backend exposes CRD management through a single generic CRD CRUD
mechanism rather than bespoke typed handlers per kind. This serves the large
set of requested kinds (8+) with one small, uniform code path.

Rejected alternatives:
- **Typed per-kind endpoints** — one handler file per kind × 4 operations is
  a large amount of boilerplate for essentially the same Kubernetes calls.
- **Raw YAML spec editing** — lets the user submit arbitrary/partial objects,
  weak validation and poor UX.

## CNPG type inventory

The `apiv1` package (cloudnative-pg v1.25.0) bundles and `AddToScheme`
registers the group `postgresql.cnpg.io,v1` with these kinds, all available
to the generic layer:
Cluster, Backup, Database, DatabaseRole, Pooler, ScheduledBackup,
ImageCatalog, ClusterImageCatalog, Publication, Subscription.

**Scoping note:** most kinds are namespaced. `ClusterImageCatalog` is
**cluster-scoped** (no namespace). `DatabaseRoleList` is the list kind of
`DatabaseRole` (namespaced). The generic layer must pass namespace only for
namespaced kinds.

## Backend design

### kube package — `internal/kube/kube.go`

Add generic CRD CRUD methods on `*Client`, operating on
`unstructured.Unstructured` with the existing CNPG scheme:

```go
type GVK struct { Kind string; Namespaced bool }

func (k *Client) ListCRD(ctx, kind, ns string) ([]unstructured.Unstructured, error)
func (k *Client) GetCRD(ctx, kind, ns, name string) (*unstructured.Unstructured, error)
func (k *Client) CreateCRD(ctx, kind, ns string, obj *unstructured.Unstructured) error
func (k *Client) UpdateCRD(ctx, kind, ns string, obj *unstructured.Unstructured) error
func (k *Client) PatchCRD(ctx, kind, ns, name string, patch map[string]any) error
func (k *Client) DeleteCRD(ctx, kind, ns, name string) error
```

- `kind` is resolved to `schema.GroupVersionKind{Group: "postgresql.cnpg.io", Version: "v1", Kind}` and namespaced/cluster-scoped via a whitelist map.
- Construct the object with the right `ObjectMeta` (namespace set only when namespaced).
- `PatchCRD` uses a JSON merge patch (`types.MergePatchType`).

### web package — `internal/web/crds.go`

Generic REST endpoints (base `/api`):

| Route | Method | Purpose |
|---|---|---|
| `/api/crds/{kind}?ns=` | GET | List CRDs of a kind (filtered by namespace if given) |
| `/api/crds/{kind}/{name}?ns=` | GET | Get one CRD as JSON |
| `/api/crds/{kind}?ns=` | POST | Create (body is the CRD JSON) |
| `/api/crds/{kind}/{name}?ns=` | PUT | Replace/update (full object) |
| `/api/crds/{kind}/{name}?ns=` | PATCH | Merge patch (body: partial object) |
| `/api/crds/{kind}/{name}?ns=` | DELETE | Delete |

- `{kind}` is validated against the whitelist; invalid → 400.
- `ns` omitted + kind namespaced → cluster-scoped list (all namespaces), so
  the shared CRD browser and the Cluster-scoped views can both use it.
- Reuse `writeError` for NotFound/Forbidden/API-server validation errors.

### Cluster edit & scale — `internal/web/clusters.go`

Because `Cluster` is the app's core entity, add two typed convenience
endpoints that reuse the generic patch mechanism:

| Route | Method | Body | Purpose |
|---|---|---|---|
| `/api/clusters/{cluster}/scale?ns=` | PATCH | `{"instances": N}` | Set `spec.instances` via merge patch |
| `/api/clusters/{cluster}/config?ns=` | PATCH | partial Cluster spec | Merge patch `spec` (resources, storage size, image, postgresql.conf) |

These resolve the cluster via existing `resolveCluster`, then
`PatchCRD(Cluster, ...)` on a targeted spec path.

### ClusterStore interface — `internal/web/server.go`

Extend `ClusterStore` with the generic CRD methods. Update the fake in
`server_test.go` to satisfy the interface and round-trip a typed or
unstructured in-memory store.

## Frontend design

### `api.ts`

- Extend `Cluster` with CRD fields surfaced from the spec: `instances`,
  `storage` (size/class), `resources` (cpu/mem), `image`, `postgresql`.
- Add a generic `crud` helper: `crud.list(kind, ns)`, `crud.get(kind, ns, name)`,
  `crud.create(kind, ns, obj)`, `crud.update(kind, ns, name, obj)`,
  `crud.patch(kind, ns, name, obj)`, `crud.delete(kind, ns, name)`.
- Add `scale(cluster, ns, instances)` and `editConfig(cluster, ns, spec)`.

### `ClusterDetail.vue` (Overview tab)

- **Scale** stepper: current `instances` with + / − buttons, persists via
  `scale` endpoint.
- **Edit** panel: storage size, cpu/mem requests/limits, image, and a
  `postgresql` config textarea; persists via `editConfig`.

### New generic CRD browser

A nav section (sidebar or a tab) listing the other CRD kinds
(Backup, Database, DatabaseRole, Pooler, ScheduledBackup, ImageCatalog,
ClusterImageCatalog). Selecting a kind lists its items; items open a
detail/edit view:

- List: name, namespace (or cluster-wide), phase/status summary.
- Detail: read-only spec/status rendered as JSON, with **Edit** (free-form
  JSON object editor → PUT) and **Delete**.
- **Create**: a small form that POSTs an object with mandatory
  `metadata.name` (and `metadata.namespace` for namespaced kinds) plus
  provided spec.

The generic JSON editor covers all kinds with one component (YAGNI: bespoke
forms per kind deferred).

## Data flow

UI form → generic REST endpoint → generic kube CRD method → validated by the
OpenAPI schema on the API server → CNPG operator reconciles the CRD and
updates `status`. Reads render CRD spec/status as JSON.

## Error handling

- Invalid kind or malformed body → 400.
- Namespaced kind listed without ns → lists across namespaces (existing
  `resolveCluster` semantics).
- Reuse existing `writeError` mappings (404 NotFound, 403 Forbidden, 500).

## Security / RBAC

Deployment RBAC must be widened so the ServiceAccount can
`get/list/watch/create/update/patch/delete` the new CRD types
(`scheduledbackups`, `poolers`, `databases`, `databaseroles`, `imagecatalogs`,
`clusterimagecatalogs` — `clusterimagecatalogs` cluster-wide) in addition to
the existing `clusters`/`backups`/`secrets`/`pods` grants. Update
`deploy/` manifests accordingly.

## Explicitly out of scope

- Create/delete of `Cluster` CRDs (per user decision).
- Bespoke per-kind create/edit forms (generic JSON editor instead).
- Any change to the SQL-driven tabs.

## Testing

- Go unit tests: generic CRD round-trip against the fake store
  (list/get/create/update/delete), cluster-scoped vs namespaced handling,
  kind whitelist rejection, and merge-patch path.
- `go build ./...`, `go vet ./...`, `go test ./...` (with `CGO_ENABLED=0`),
  and frontend `vite build` / `vue-tsc` must pass.
- No live Talos cluster here — CRD reconciliation and RBAC validated by
  review only; user deploys and reports.
