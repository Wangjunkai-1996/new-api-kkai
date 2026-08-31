WEB_DIR = ./web/default
WEB_CLASSIC_DIR = ./web/classic
API_DIR = .
VERSION_FILE ?= VERSION
APP_VERSION ?= $(strip $(shell cat $(VERSION_FILE) 2>/dev/null))
DEV_WEB_DEFAULT_PORT ?= 5173
DEV_WEB_CLASSIC_PORT ?= 5174
DEV_COMPOSE_FILE = docker-compose.dev.yml
DEV_POSTGRES_SERVICE = postgres
DEV_API_SERVICE = new-api
DEV_POSTGRES_DB = new-api
DEV_POSTGRES_USER = root
DEV_SQLITE_PATH ?= one-api.db

.PHONY: all web-install build-web build-web-classic build-all-web build-backend-only test-backend-only test-frontend-build start-api dev dev-api dev-api-rebuild dev-web dev-web-classic reset-setup test-manual-deploy test-standby-sync

all: build-all-web start-api

web-install:
	@echo "Installing web dependencies..."
	@cd ./web && bun install --frozen-lockfile --network-concurrency=1 --concurrent-scripts=1

build-web: web-install
	@echo "Building default web..."
	@cd $(WEB_DIR) && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION='$(APP_VERSION)' bun run build

build-web-classic: web-install
	@echo "Building classic web..."
	@cd $(WEB_CLASSIC_DIR) && VITE_REACT_APP_VERSION='$(APP_VERSION)' bun run build

build-all-web: build-web build-web-classic

# Compile the API without a frontend tree. The external build tag supplies
# empty embed symbols; production's default target remains self-contained.
BACKEND_ONLY_OUTPUT ?= .local-releases/backend-only/new-api

build-backend-only:
	@mkdir -p "$(dir $(BACKEND_ONLY_OUTPUT))"
	@go build -tags=external_frontend -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(APP_VERSION)'" -o "$(BACKEND_ONLY_OUTPUT)" .

test-backend-only:
	@go test -tags=external_frontend .

test-frontend-build:
	@bash scripts/kkai/frontend-build-release_test.sh

start-api:
	@echo "Starting api dev server..."
	@cd $(API_DIR) && go run . &

dev-api:
	@echo "Starting api services (docker)..."
	@docker compose -f $(DEV_COMPOSE_FILE) up -d

dev-api-rebuild:
	@echo "Rebuilding and starting api service (docker)..."
	@docker compose -f $(DEV_COMPOSE_FILE) up -d --build $(DEV_API_SERVICE)

dev-web:
	@echo "Starting default web dev server..."
	@echo "Default web: http://localhost:$(DEV_WEB_DEFAULT_PORT)"
	@cd ./web && bun install --filter ./default --network-concurrency=1 --concurrent-scripts=1
	@cd $(WEB_DIR) && bun run dev -- --host 0.0.0.0 --port $(DEV_WEB_DEFAULT_PORT)

dev-web-classic:
	@echo "Starting classic web dev server..."
	@cd ./web && bun install --filter ./classic --network-concurrency=1 --concurrent-scripts=1
	@cd $(WEB_CLASSIC_DIR) && bun run dev -- --host 0.0.0.0 --port $(DEV_WEB_CLASSIC_PORT)

dev: dev-api dev-web

reset-setup:
	@echo "Resetting local setup wizard state..."
	@if docker compose -f $(DEV_COMPOSE_FILE) ps --services --status running | grep -qx "$(DEV_POSTGRES_SERVICE)"; then \
		echo "Detected running docker dev PostgreSQL. Removing setup record and root users..."; \
		docker compose -f $(DEV_COMPOSE_FILE) exec -T $(DEV_POSTGRES_SERVICE) \
			psql -U $(DEV_POSTGRES_USER) -d $(DEV_POSTGRES_DB) \
			-c 'DELETE FROM setups;' \
			-c 'DELETE FROM users WHERE role = 100;' \
			-c "DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "Restarting docker dev api so setup status is recalculated..."; \
		docker compose -f $(DEV_COMPOSE_FILE) restart $(DEV_API_SERVICE); \
	elif db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; db_path="$${db_path%%\?*}"; [ -f "$$db_path" ]; then \
		db_path="$${SQLITE_PATH:-$(DEV_SQLITE_PATH)}"; \
		db_path="$${db_path%%\?*}"; \
		echo "Detected local SQLite database: $$db_path"; \
		sqlite3 "$$db_path" \
			"DELETE FROM setups; DELETE FROM users WHERE role = 100; DELETE FROM options WHERE key IN ('SelfUseModeEnabled', 'DemoSiteEnabled');"; \
		echo "SQLite setup state reset. Restart the local api process before testing the setup wizard."; \
	else \
		echo "No running docker dev PostgreSQL or local SQLite database found."; \
		echo "Start the dev stack with 'make dev-api', or set SQLITE_PATH/DEV_SQLITE_PATH to your local SQLite database."; \
		exit 1; \
	fi

test-standby-sync:
	@bash scripts/kkai/test-standby-sync.sh

test-manual-deploy:
	@bash scripts/kkai/build-manual-release_test.sh
	@bash scripts/kkai/deploy-manual-release_test.sh
