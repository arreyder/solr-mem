.PHONY: build build-indexer build-bench install install-indexer test tidy run dev up down logs reset docker-build docker-up config systemd-install systemd-uninstall indexer-install indexer-uninstall launchd-install launchd-install-server launchd-install-indexer launchd-uninstall skill-install skill-uninstall bench

# Go targets
build:
	go build -o bin/solr-mem-server ./cmd/solr-mem-server

build-indexer:
	go build -o bin/solr-mem-indexer ./cmd/solr-mem-indexer

build-all: build build-indexer build-bench

build-bench:
	go build -o bin/solr-mem-bench ./cmd/solr-mem-bench

# Retrieval benchmark. Seeds the memories collection with namespaced bench-*
# docs (safe to run against a live collection — only touches bench-* IDs) and
# runs the shipped query set. Override BENCH_URL to point elsewhere.
BENCH_URL ?= http://pax89.local:8983/solr/memories
bench: build-bench
	./bin/solr-mem-bench -solr-url $(BENCH_URL) -seed \
	  -corpus cmd/solr-mem-bench/testdata/corpus.jsonl \
	  -queries cmd/solr-mem-bench/testdata/queries.jsonl

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

# macOS launchd services
launchd-install-server: build
	mkdir -p ~/Library/LaunchAgents
	@echo '<?xml version="1.0" encoding="UTF-8"?>' > ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '<plist version="1.0"><dict>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>Label</key><string>com.solr-mem.server</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>ProgramArguments</key><array>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '    <string>'$$(go env GOPATH)'/bin/solr-mem-server</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '    <string>--http</string><string>:8080</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  </array>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>EnvironmentVariables</key><dict>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '    <key>SOLR_URL</key><string>http://localhost:8983/solr/memories</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '    <key>SOLR_URL_CODE</key><string>http://localhost:8983/solr/code</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  </dict>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>KeepAlive</key><true/>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>RunAtLoad</key><true/>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>StandardOutPath</key><string>/tmp/solr-mem-server.log</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '  <key>StandardErrorPath</key><string>/tmp/solr-mem-server.log</string>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	@echo '</dict></plist>' >> ~/Library/LaunchAgents/com.solr-mem.server.plist
	go install ./cmd/solr-mem-server
	launchctl load ~/Library/LaunchAgents/com.solr-mem.server.plist

launchd-install-indexer: build-indexer
	mkdir -p ~/Library/LaunchAgents
	@echo '<?xml version="1.0" encoding="UTF-8"?>' > ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '<plist version="1.0"><dict>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>Label</key><string>com.solr-mem.indexer</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>ProgramArguments</key><array>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <string>'$$(go env GOPATH)'/bin/solr-mem-indexer</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <string>--watch</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  </array>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>EnvironmentVariables</key><dict>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>SOLR_URL_CODE</key><string>http://localhost:8983/solr/code</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>CLONE_DIR</key><string>'$$HOME'/solr-mem-repos</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>INDEX_BRANCH</key><string>main</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>INDEX_INTERVAL</key><string>300</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>INDEX_REPOS</key><string>$(INDEX_REPOS)</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '    <key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  </dict>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>KeepAlive</key><true/>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>RunAtLoad</key><true/>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>StandardOutPath</key><string>/tmp/solr-mem-indexer.log</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '  <key>StandardErrorPath</key><string>/tmp/solr-mem-indexer.log</string>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	@echo '</dict></plist>' >> ~/Library/LaunchAgents/com.solr-mem.indexer.plist
	go install ./cmd/solr-mem-indexer
	launchctl load ~/Library/LaunchAgents/com.solr-mem.indexer.plist

launchd-uninstall:
	launchctl unload ~/Library/LaunchAgents/com.solr-mem.server.plist 2>/dev/null || true
	launchctl unload ~/Library/LaunchAgents/com.solr-mem.indexer.plist 2>/dev/null || true
	rm -f ~/Library/LaunchAgents/com.solr-mem.server.plist
	rm -f ~/Library/LaunchAgents/com.solr-mem.indexer.plist

launchd-install: launchd-install-server launchd-install-indexer

# Claude Code skill
skill-install:
	mkdir -p ~/.claude/skills
	ln -sfn $(CURDIR)/skills/solr-mem ~/.claude/skills/solr-mem

skill-uninstall:
	rm -f ~/.claude/skills/solr-mem

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
