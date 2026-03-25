.PHONY: build install test tidy run dev up down logs reset docker-build docker-up config

# Go targets
build:
	go build -o bin/solr-mem-server ./cmd/solr-mem-server

install:
	go install ./cmd/solr-mem-server

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
	SOLR_URL=http://pax89.local:8983/solr/memories go run ./cmd/solr-mem-server

# Docker full stack (includes MCP server in container)
docker-build:
	docker compose --profile network build

docker-up:
	docker compose --profile network up -d

# Print Claude Code MCP config
config:
	@echo '{'
	@echo '  "mcpServers": {'
	@echo '    "solr-mem": {'
	@echo '      "command": "'$$(go env GOPATH)'/bin/solr-mem-server",'
	@echo '      "env": {'
	@echo '        "SOLR_URL": "http://pax89.local:8983/solr/memories"'
	@echo '      }'
	@echo '    }'
	@echo '  }'
	@echo '}'
