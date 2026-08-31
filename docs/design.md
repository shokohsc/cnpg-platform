# Design: CNPG Manager — a Supabase-style UI for CloudNativePG clusters

Date: 2026-08-31
Status: Approved (design gate passed)

## Purpose

A homelab user runs a Talos Kubernetes cluster with applications that each
need a Postgres connection URL. CloudNativePG (CNPG) is installed and manages
one or more Postgres clusters. This project provides a Go backend + Vue
frontend that gives a "Supabase experience" for those clusters: browse
clusters, provision databases and roles, browse tables, run SQL, and copy a
ready-made connection URL for any app.

The app runs **in-cluster** and authenticates to Kubernetes via a
ServiceAccount. The UI is **unauthenticated** (LAN homelab, no login gate).

## Approach (selected: A)

**Live-derived, SQL-executing backend.** The backend lists CNPG `Cluster`
CRDs via the Kubernetes API and reads each cluster's `-superuser` secret.
It connects directly to that cluster's `-rw` service and executes SQL against
the live primary. Databases, roles, schemas, and tables are discovered from
`pg_catalog` on every request — no application-side persistence, no
controllers, no drift.

Rejected alternatives:
- **B. Custom CRDs + controller** — reconciliation machinery for what is
  ultimately `CREATE DATABASE`. Over-engineered.
- **C. initdb-declared databases only** — CRD-declared DBs exist only at
  bootstrap; cannot provision after the fact. Fails the core requirement.

## Runtime environment

- Language: Go (stdlib `net/http`, `client-go`, `pgx/v5`).
- Frontend: Vue 3 + Vite + Tailwind, dark Supabase-style theme.
- Frontend build embedded into the Go binary via `go:embed` — single
  artifact, easiest in-cluster deployment.
- No database/metadata store. Everything derived live.

## HTTP API

Base path `/api`. All responses JSON.

| Route | Method | Purpose |
|---|---|---|
| `/api/clusters` | GET | All CNPG clusters across namespaces, each with status phase, ready pods/replicas, PH version, and live db/role counts. |
| `/api/clusters/{c}` | GET | Cluster detail (name, ns, version, endpoints, spec essentials). |
| `/api/clusters/{c}/databases` | GET | Databases from `pg_database` (name, owner, size, encoding, template info, access privileges). |
| `/api/clusters/{c}/databases` | POST | Create DB. Body: name, optional owner role, optional template, optional encoding/locale. |
| `/api/clusters/{c}/databases/{db}` | DELETE | Drop DB. First `pg_terminate_backend` all sessions, then `DROP DATABASE`. |
| `/api/clusters/{c}/roles` | GET | Roles from `pg_roles` (name, attributes, member of, owned databases). |
| `/api/clusters/{c}/roles` | POST | Create role + password. Password returned **once** in the response. Body: name, password (optional generate), attributes (login/superuser/createdb), optional DB grants. |
| `/api/clusters/{c}/roles/{role}` | DELETE | `REASSIGN OWNED` then `DROP OWNED`, then `DROP ROLE`. |
| `/api/clusters/{c}/sql` | POST | Run SQL against a named db. Body: db, statement, readOnly flag. SELECT / RETURNING → column names + rows + rowcount; other statements → command tag + rowcount. |
| `/api/clusters/{c}/tables` | GET | Schemas, tables, columns (name/type/nullable/default), primary/foreign keys, indexes, row estimates. |
| `/api/clusters/{c}/tables/{schema}.{table}/rows` | GET | Row browser. Query params: limit (default 50, max 500), offset. |
| `/api/clusters/{c}/backups` | GET | CNPG `Backup` CRs for the cluster (name, backup kind/physical, phase, started/finished, method, target, destination). |
| `/api/clusters/{c}/backups` | POST | Create an on-demand CNPG `Backup` CR (method: `barmanObjectStore`, doesn't require target design). |
| `/api/clusters/{c}/connect` | GET | Component parts + full connection URL(s) for a chosen database and role, with direct/sslmode variants. |

### Connect URL model

For a given cluster, database, and role:
- Host = `{cluster}-rw.{ns}.svc`, port = 5432 (CNPG convention; optional
  per-cluster override via annotation `cnpg-manager.io/port`).
- Direct URL: `postgresql://{user}:{pass}@{host}:{port}/{db}?sslmode=require`.
- A lightweight SSL-comment/stanza describes CA verification (CNPG TLS is on
  by default); the copyable default is `sslmode=require` since app containers
  inside the cluster don't need cert validation against a CA they trust.
  A `sslmode=verify-full` variant with the cluster CA secret path is shown.
- Non-superuser role passwords are read from the database role itself when
  the role is app-managed; superuser connections use the CNPG superuser secret.

## Connectivity details

- `client-go` uses in-cluster config. Out-of-cluster fallback (kubeconfig)
  is accepted if present for local development.
- Only clusters with the CNPG annotation/API-GROUP `cluster.cnpg.io` are
  targeted.
- The superuser secret is `{cluster}-superuser` in the cluster's namespace;
  its `username`/`password` keys are used for management connections.
- TLS: CNPG issues a per-cluster CA secret (`{cluster}-ca`). The backend
  connects with `sslmode=require` using a pooled cert pool built from the CA
  secret (fall back to InsecureSkipVerify only when the CA secret is
  malformed — flagged in code).
- Role passwords for app-managed roles are stored at provisioning time in a
  Kubernetes secret `{cluster}-{role}` in the cluster's namespace so the
  Connect modal can render a working URL for non-superuser roles. Superuser
  connections use the CNPG superuser secret. The password is returned to the
  caller exactly once, at creation.
- Every SQL sink is scoped to one cluster and (for db-level ops) one
  database. A pool of one *management* connection per cluster is reused;
  strictly sequential per cluster (no concurrent DDL on one cluster;
  `ponytail:` per-cluster pool, expand to a small queue/connection pool if
  concurrent admin usage appears).

## Security posture

- The backend holds superuser-equivalent power over all discovered clusters.
  This is by design for a homelab admin tool.
- RBAC restricts its ability to *only* the cluster object types and secrets
  it needs (see Deployment).
- User-supplied identifiers (db names, role names, schema/table names) are
  **always passed as quoted SQL literals/identifiers**, never interpolated
  raw. SQL editor statements are user-authored by design.
- No secrets are logged. Passwords returned once and only to the caller.

## Error handling

- PG errors mapped to JSON with the `PGCODE`, friendly `message`, and a
  `detail` whenever available (e.g. duplicate database, permission denied,
  objects in use).
- k8s API errors mapped to the corresponding HTTP status (404 cluster =
  NotFound, forbidden = 403).
- Drop-database and drop-role are two-phase with clear failure messages when
  the first phase can't complete.

## Frontend

- Vue 3 `<script setup>` + Vite + Tailwind (v3-era config; no component
  framework — hand-rolled small components keep it lean).
- Single-page layout: dark background, left sidebar with cluster list /
  selected cluster, main pane switches between views.
- Views:
  - **Clusters** — card grid; name, namespace, version, status pill
    (Ready/Degraded), db count, backup last-run. Click → detail.
  - **Databases** — table of DBs; create dialog (name, owner, template,
    encoding); row actions: drop, and "Connect" (opens connect modal).
  - **Roles** — table of roles; create dialog (name, password, attributes);
    show-credential flash on creation; drop action.
  - **SQL Editor** — db selector, statement editor, run; results grid with
    monospaced cells; read-only toggle. Keyboard: Ctrl/Cmd+Enter to run.
  - **Tables** — schema tree → table view: columns, indexes, keys, and a row
    browser (paginated). No inline editing (explicitly out of scope).
  - **Backups** — list of backup CRs; "Backup now" button.
  - **Connect modal** — database + role selectors, live-edit host/port,
    full URL in a copy box + parts breakdown, works from any view.
- Frontend talks to the same origin (`/api`), so the built assets embed
  cleanly behind one Deployment/Service.

## Deployment

`deploy/` — plain YAML (kustomize-compatible, `kustomization.yaml` provided):
- `Namespace` with a note the app namespace should be where CNPG lives or a
  dedicated one (namespaces configurable via `KUBE_NAMESPACES` env, default
  `*`).
- `ServiceAccount`, `Role` (namespaced: its own ns) + `ClusterRole` +
  `ClusterRoleBinding` granting:
  - `get/list/watch clusters.postgresql.cnpg.io` and
    `backups.postgresql.cnpg.io` (all namespaces)
  - `get/list/watch secrets` (all namespaces) — to read superuser/CA/app-role
    secrets
  - `get/list/watch pods` (all namespaces) — to compute ready replicas
  - `create backups.postgresql.cnpg.io` — on-demand backups
- `Deployment` — single replica, `POD_NAMESPACE` env for RBAC self-reference;
  image `ghcr.io/{user}/cnpg-manager`.
- `Service` (ClusterIP) + commented-out `Ingress` example for a reverse proxy.

## Explicitly out of scope

- Inline row/table editing (SQL editor covers it).
- Metrics / CPU-memory dashboards.
- Full restore-from-backup (read-only backup visibility + on-demand backup
  trigger only).
- Connection pooler (PgBouncer) deployment/manangement.
- Authn/authz for the UI, multi-tenant isolation, audit logging.

## Testing

- Go unit tests for: DB/role name quoting, connect-URL generation, PG error
  mapping, read-only transaction enforcement for the SQL editor.
- `go vet ./...`, `go build`, `go test ./...` must pass.
- Frontend: `vite build` must succeed; `vue-tsc` type-check if configured.
- No live Talos cluster is available in this environment — RBAC and deploy
  manifests are validated by review only; user deploys and reports.