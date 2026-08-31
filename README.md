# cnpg-manager

A lightweight web UI for managing CloudNativePG PostgreSQL clusters, databases, users, backups, and connection strings.

## Quick build

```bash
make frontend && make build   # → bin/cnpg-manager
```

## Local dev

```bash
make frontend
go run ./cmd/server           # connects via kubeconfig, listens on :8080
```

## In-cluster deploy

```bash
make image                    # → ghcr.io/YOURUSER/cnpg-manager:latest
make deploy                   # kubectl apply -k deploy
```

Edit `deploy/kustomization.yaml` to set your registry image before deploying.

## RBAC

The ClusterRole grants access to:

- **clusters/backups** (`postgresql.cnpg.io`): read and create — lists CNPG clusters, views backup status, triggers on-demand backups.
- **secrets/pods** (core): read and create/update — reads connection URLs, reads pod info, writes generated secrets.

Access is cluster-wide; the app reads/writes secrets in any namespace a cluster is deployed to.

## Consuming a connection URL

1. Open the app → **Clusters** tab → click **Connect** on a cluster.
2. Copy the connection URL from the modal.
3. Create or edit a Kubernetes Secret in the app namespace:
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: my-app-db
   type: Opaque
   stringData:
     DATABASE_URL: "postgresql://user:pass@host:5432/db"
   ```
4. Mount `my-app-db.DATABASE_URL` as an env var in your workload.

## Notes

- Builds a static distroless image (~15 MB).
- Serves an embedded Vue SPA from the Go binary.
