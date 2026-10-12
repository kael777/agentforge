SHELL := bash

BACKEND_DIR := backend/gofiber-gorm-next
FRONTEND_DIR := frontend/web
COMPOSE_FILE := devops/docker/docker-compose.yml
COMPOSE_PROJECT := agentforge

.PHONY: help bootstrap dev dev-infra dev-backend dev-frontend down test lint build check release-check publish

help:
	@echo "AgentForge development commands"
	@echo "  make bootstrap             Install Go and npm dependencies"
	@echo "  make dev                   Start MySQL, migrations, Go API and React"
	@echo "  make test                  Run backend and frontend tests"
	@echo "  make lint                  Run go vet and frontend lint"
	@echo "  make build                 Build backend targets and React dist"
	@echo "  make check                 Run lint, tests and production builds"
	@echo "  make down                  Stop local Docker services"
	@echo "  make publish VERSION=v0.1.0  Validate, tag and trigger GitHub Release"

bootstrap:
	cd $(BACKEND_DIR) && go mod download
	cd $(FRONTEND_DIR) && npm ci

dev:
	bash devops/scripts/dev.sh

dev-infra:
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) up -d --wait mysql

dev-backend:
	cd $(BACKEND_DIR) && go run ./cmd/api

dev-frontend:
	cd $(FRONTEND_DIR) && npm run dev

down:
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) down

test:
	bash devops/scripts/test.sh

lint:
	cd $(BACKEND_DIR) && go vet ./...
	cd $(FRONTEND_DIR) && npm run lint

build:
	bash devops/scripts/build-all.sh

check: lint test build

release-check:
	@test -n "$(VERSION)" || (echo "VERSION is required, for example: make publish VERSION=v0.1.0" >&2; exit 2)
	@case "$(VERSION)" in v[0-9]*.[0-9]*.[0-9]*) ;; *) echo "VERSION must look like v0.1.0" >&2; exit 2 ;; esac
	@test -z "$$(git status --porcelain)" || (echo "Git working tree must be clean before publishing" >&2; exit 2)
	$(MAKE) check

publish: release-check
	git tag -a "$(VERSION)" -m "AgentForge $(VERSION)"
	git push origin "$(VERSION)"
