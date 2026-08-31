# AGENTS.md

## Build Commands

```bash
make frontend && make build   # Full build → bin/cnpg-manager
make test                     # go vet ./... && go test ./internal/...
make image                    # Docker build → ghcr.io/YOURUSER/cnpg-manager:latest
make deploy                   # kubectl apply -k deploy
```

## Local Development

- **Backend:** `go run ./cmd/server` (connects via kubeconfig, listens on :8080)
- **Frontend:** `cd frontend && npm run dev` (Vite dev server, proxies /api to :8080)
## Architecture

- **Go backend** (`cmd/server/main.go`): HTTP server with embedded Vue SPA
- **Vue 3 + TypeScript frontend** (`frontend/`): Vite build outputs to `internal/web/dist/`
- **Three internal packages:**
  - `internal/kube/`: Kubernetes client (CNPG CRDs, secrets, pods)
  - `internal/pg/`: PostgreSQL operations (databases, roles, SQL)
  - `internal/web/`: HTTP handlers, SPA serving, API routes

## Key Conventions

- Frontend dist is embedded into Go binary via `//go:embed all:dist`
- Tests use fakes (fakePG, fakeStore) — no test frameworks beyond stdlib
- RBAC grants cluster-wide access to CNPG clusters, secrets, pods
- No linting/formatting tools configured — follow existing style
- Deploy uses kustomize (`deploy/kustomization.yaml`)
