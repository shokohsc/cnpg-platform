FROM node:18-alpine AS frontend
WORKDIR /fe
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/cnpg-manager ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/cnpg-manager /cnpg-manager
EXPOSE 8080
ENTRYPOINT ["/cnpg-manager"]
