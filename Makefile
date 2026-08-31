.PHONY: up down logs ps migrate seed run build vet fmt tidy clean

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

# 一键复位：删库重建 → migrate → seed
reset:
	docker compose down -v
	docker compose up -d
	@echo "waiting for MySQL..."
	@for i in $$(seq 1 30); do \
	  h=$$(docker inspect -f '{{.State.Health.Status}}' gorush-mysql 2>/dev/null); \
	  if [ "$$h" = "healthy" ]; then echo "MySQL ready"; break; fi; \
	  sleep 1; \
	done
	$(MAKE) migrate seed