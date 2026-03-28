.PHONY: build build-indexer install install-indexer test tidy run dev up down logs reset docker-build docker-up config systemd-install systemd-uninstall indexer-install indexer-uninstall

# Go targets
build:
	go build -o bin/solr-mem-server ./cmd/solr-mem-server

build-indexer:
	go build -o bin/solr-mem-indexer ./cmd/solr-mem-indexer

build-all: build build-indexer

install:
	go install ./cmd/solr-mem-server

install-indexer:
	go install ./cmd/solr-mem-indexer

install-all: install install-indexer

test:
	go test ./...

tidy:
	go mod tidy

run:
	go run ./cmd/solr-mem-server

# Docker targets (Solr only)
up:
	docker compose up -d solr

down:
	docker compose down

logs:
	docker compose logs -f

reset:
	docker compose down -v

# Development: start Solr + run MCP server locally
dev: up
	@echo "Waiting for Solr to be healthy..."
	@until curl -sf http://pax89.local:8983/solr/memories/admin/ping > /dev/null 2>&1; do sleep 1; done
	@echo "Solr ready. Starting MCP server..."
	SOLR_URL=http://pax89.local:8983/solr/memories SOLR_URL_CODE=http://pax89.local:8983/solr/code go run ./cmd/solr-mem-server

# Index a local repository (one-shot)
index: build-indexer
	SOLR_URL_CODE=http://pax89.local:8983/solr/code ./bin/solr-mem-indexer --repo $(REPO) --once

# Index with watch mode
index-watch: build-indexer
	SOLR_URL_CODE=http://pax89.local:8983/solr/code ./bin/solr-mem-indexer --repo $(REPO) --watch

# Docker full stack (includes MCP server in container)
docker-build:
	docker compose --profile network build

docker-up:
	docker compose --profile network up -d

# Systemd user service - MCP server
systemd-install: build
	mkdir -p ~/.config/systemd/user
	cp solr-mem-server.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable --now solr-mem-server.service

systemd-uninstall:
	systemctl --user disable --now solr-mem-server.service || true
	rm -f ~/.config/systemd/user/solr-mem-server.service
	systemctl --user daemon-reload

# Systemd user service - Indexer
indexer-install: build-indexer
	mkdir -p ~/.config/systemd/user
	cp solr-mem-indexer.service ~/.config/systemd/user/
	systemctl --user daemon-reload
	systemctl --user enable --now solr-mem-indexer.service

indexer-uninstall:
	systemctl --user disable --now solr-mem-indexer.service || true
	rm -f ~/.config/systemd/user/solr-mem-indexer.service
	systemctl --user daemon-reload

# Print Claude Code MCP config
config:
	@echo '{'
	@echo '  "mcpServers": {'
	@echo '    "solr-mem": {'
	@echo '      "command": "'$$(go env GOPATH)'/bin/solr-mem-server",'
	@echo '      "env": {'
	@echo '        "SOLR_URL": "http://pax89.local:8983/solr/memories",'
	@echo '        "SOLR_URL_CODE": "http://pax89.local:8983/solr/code"'
	@echo '      }'
	@echo '    }'
	@echo '  }'
	@echo '}'
