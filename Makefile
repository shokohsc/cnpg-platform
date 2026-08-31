.PHONY: build frontend test image deploy

build:
	go build -o bin/cnpg-manager ./cmd/server

frontend:
	cd frontend && npm ci && npm run build

test:
	go vet ./... && go test ./internal/...

image:
	docker build -t ghcr.io/YOURUSER/cnpg-manager:latest .

deploy:
	kubectl apply -k deploy
