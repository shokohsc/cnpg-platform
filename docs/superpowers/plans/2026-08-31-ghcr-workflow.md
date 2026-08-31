# GitHub Actions GHCR Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub Actions workflow that builds the multi-stage Docker image and pushes it to GitHub Container Registry (ghcr.io) on pushes to `main` and version tags.

**Architecture:** Single workflow file using official `docker/*` GitHub Actions. Triggers on push to `main` branch and semver tags (`v*`). Uses `docker/metadata-action` for tag strategy and `docker/build-push-action` for the build. Authenticates to ghcr.io via `GITHUB_TOKEN`.

**Tech Stack:** GitHub Actions, Docker Buildx, docker/metadata-action, docker/build-push-action

**Spec:** Design discussed in chat — triggers on push to main + tags, pushes to ghcr.io, AMD64 only

## Global Constraints

- Image name: `ghcr.io/${{ github.repository }}`
- Dockerfile: multi-stage (node:18-alpine frontend → golang:1.24-alpine backend → distroless runtime)
- No multi-platform builds (AMD64 only)
- Uses `GITHUB_TOKEN` for GHCR auth (no secrets needed)

---

### Task 1: Create GitHub Actions Workflow

**Files:**
- Create: `.github/workflows/build-and-push.yml`

**Interfaces:**
- Consumes: existing `Dockerfile` at repo root
- Produces: container image pushed to `ghcr.io/<owner>/cnpg-manager`

- [ ] **Step 1: Create workflow directory**

```bash
mkdir -p .github/workflows
```

- [ ] **Step 2: Create the workflow file**

Create `.github/workflows/build-and-push.yml` with the following content:

```yaml
name: Build and Push Container Image

on:
  push:
    branches: [main]
    tags: ["v*"]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata (tags, labels)
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
            type=sha,prefix=

      - name: Build and push Docker image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 3: Verify workflow YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/build-and-push.yml'))"
```

Expected: No output (valid YAML)

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/build-and-push.yml
git commit -m "ci: add GitHub Actions workflow to build and push to GHCR"
```
