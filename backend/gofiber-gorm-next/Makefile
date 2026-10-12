COMPOSE_FILE := ../../devops/docker/docker-compose.yml
COMPOSE_PROJECT := agentforge

.PHONY: fmt tidy test build run migrate compose-up compose-down

fmt:
	gofmt -w ./cmd ./internal

tidy:
	go mod tidy

test:
	go test ./...

build:
	go build -trimpath -o bin/api ./cmd/api
	go build -trimpath -o bin/migrate ./cmd/migrate

run:
	go run ./cmd/api

migrate:
	go run ./cmd/migrate

compose-up:
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) up --build

compose-down:
	docker compose -p $(COMPOSE_PROJECT) -f $(COMPOSE_FILE) down

