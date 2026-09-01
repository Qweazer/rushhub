.PHONY: up down logs ps migrate seed reindex run build vet fmt tidy clean test-redis verify

# ---------- Docker ----------
up:
	docker compose up -d
	@docker compose ps

down:
	docker compose down

logs:
	docker compose logs -f mysql

ps:
	docker compose ps

# ---------- Go ----------
fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

build:
	go build -o ./bin/server ./cmd/server

run:
	go run ./cmd/server

# ---------- DB ----------
migrate:
	go run ./cmd/migrate

seed:
	go run ./cmd/seed

reindex:
	go run ./cmd/reindex

# ---------- Verification ----------
test-redis:
	REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -count=1

verify:
	@set -e; \
	tmp_dir=$$(mktemp -d); \
	trap 'rm -rf "$$tmp_dir"' EXIT; \
	go test ./...; \
	go vet ./...; \
	go build -o "$$tmp_dir/server" ./cmd/server; \
	go build -o "$$tmp_dir/migrate" ./cmd/migrate; \
	go build -o "$$tmp_dir/seed" ./cmd/seed; \
	go build -o "$$tmp_dir/reindex" ./cmd/reindex

# 一键复位：删库重建 → migrate → seed → reindex
reset:
	docker compose down -v
	docker compose up -d
	@echo "waiting for MySQL and Redis..."
	@for i in $$(seq 1 30); do \
	  mysql=$$(docker inspect -f '{{.State.Health.Status}}' gorush-mysql 2>/dev/null); \
	  redis=$$(docker inspect -f '{{.State.Health.Status}}' gorush-redis 2>/dev/null); \
	  if [ "$$mysql" = "healthy" ] && [ "$$redis" = "healthy" ]; then echo "MySQL and Redis ready"; exit 0; fi; \
	  sleep 1; \
	done; \
	echo "MySQL and Redis did not become ready" >&2; exit 1
	$(MAKE) migrate seed reindex
