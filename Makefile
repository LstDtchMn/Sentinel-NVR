.PHONY: dev build run test clean docker-up docker-down docker-build lint help

# ---- Variables ----
BINARY     := sentinel
BACKEND    := ./backend
MAIN       := $(BACKEND)/cmd/sentinel
CONFIG     := ./configs/sentinel.yml

# ---- Development ----
dev: ## Run backend locally (no Docker)
	cd $(BACKEND) && go run ./cmd/sentinel -config ../$(CONFIG)

build: ## Build the backend binary
	cd $(BACKEND) && CGO_ENABLED=0 go build -ldflags="-s -w" -o ../bin/$(BINARY) ./cmd/sentinel

run: build ## Build and run locally
	./bin/$(BINARY) -config $(CONFIG)

test: ## Run all backend tests
	cd $(BACKEND) && go test ./... -v

lint: ## Run linter
	cd $(BACKEND) && go vet ./...

clean: ## Remove build artifacts
	rm -rf bin/

# ---- Docker ----
docker-up: ## Start all services with Docker Compose
	docker compose up --build -d

docker-down: ## Stop all services
	docker compose down

docker-build: ## Build Docker images without starting
	docker compose build

docker-logs: ## Tail logs from all services
	docker compose logs -f

docker-gpu-nvidia: ## Start with NVIDIA GPU (uncomment deploy block in docker-compose.yml first)
	docker compose up --build -d

# ---- Frontend ----
frontend-dev: ## Start frontend dev server
	cd frontend && npm run dev

frontend-build: ## Build frontend for production
	cd frontend && npm run build

frontend-install: ## Install frontend dependencies
	cd frontend && npm ci

# ---- Help ----
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
