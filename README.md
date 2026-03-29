# solr-mem

AI agent memory and code knowledge base backed by Apache Solr. Provides an MCP server with tools for persistent memory storage and pre-analyzed code search across indexed git repositories.

## What it does

**Memory store** — Agents store and retrieve structured memories (observations, decisions, facts) with full-text search, faceted filtering, similarity matching, and automatic expiration.

**Code knowledge base** — A repo indexer processes git repositories into pre-analyzed, agent-optimized documents. Instead of reading raw source files, agents query structured YAML summaries with extracted call graphs, type references, and cross-references. For a typical code understanding task, this uses **94x fewer tokens** and is **430x faster** than grep + file reads.

### Document hierarchy

Repos are broken into four linked document levels:

| Level | Content | Format |
|-------|---------|--------|
| **Repository** | Languages, structure, stats | YAML |
| **Package** | Key types, entry points, interfaces, implementations | YAML |
| **File** | Grouped symbols, categorized imports, interface hints | YAML |
| **Symbol** | Params, returns, calls, types_used, called_by | YAML + raw source |

Cross-references (who calls what, interface implementations, method sets) are resolved in a second pass after indexing.

## Components

| Component | Description |
|-----------|-------------|
| `solr-mem-server` | MCP server (stdio or HTTP) with 14 tools |
| `solr-mem-indexer` | Git repo indexer with watch mode for incremental updates |
| Solr 9 | Search engine with two collections: `memories` and `code` |

## Quick start

```bash
# Start Solr
make up

# Build both binaries
make build-all

# Run MCP server (stdio mode for Claude Code)
SOLR_URL=http://localhost:8983/solr/memories \
SOLR_URL_CODE=http://localhost:8983/solr/code \
  ./bin/solr-mem-server

# Index a repository
SOLR_URL_CODE=http://localhost:8983/solr/code \
CLONE_DIR=~/solr-mem-repos \
  ./bin/solr-mem-indexer --repo /path/to/repo --once

# Watch mode (polls for new commits every 5 minutes)
SOLR_URL_CODE=http://localhost:8983/solr/code \
CLONE_DIR=~/solr-mem-repos \
INDEX_REPOS=git@github.com:org/repo.git \
  ./bin/solr-mem-indexer --watch
```

## MCP tools

### Memory tools

| Tool | Description |
|------|-------------|
| `store_memory` | Store a memory with content, tags, type, importance, lifetime |
| `search_memories` | Full-text search with filters, highlighting, facets |
| `update_memory` | Atomic field updates on an existing memory |
| `delete_memory` | Delete a memory by ID |
| `list_memories` | Browse memories with sorting and facets |
| `similar_memories` | Find related memories using MoreLikeThis |
| `memory_stats` | Counts by type, agent, source, lifetime |
| `bulk_store_memories` | Batch insert multiple memories |
| `relate_memories` | Bidirectional linking between memories |

### Code tools

| Tool | Description |
|------|-------------|
| `search_code` | Full-text search across indexed repos with code-aware boosting |
| `get_symbol` | Fetch a specific function/type by name with source code |
| `browse_code` | Navigate the repo > package > file > symbol hierarchy |
| `code_context` | Get file summary + symbols at a specific location |
| `code_context_bundle` | Complete working set: symbol + callees + callers + types + package context |

## Claude Code setup

```bash
# Add as HTTP MCP server (recommended — runs on a dedicated host)
claude mcp add -s user --transport http solr-mem http://your-host:8080/mcp

# Or as stdio (local, no HTTP)
claude mcp add -s user \
  -e SOLR_URL=http://localhost:8983/solr/memories \
  -e SOLR_URL_CODE=http://localhost:8983/solr/code \
  solr-mem -- /path/to/solr-mem-server
```

## Configuration

### solr-mem-server

| Env var | Default | Description |
|---------|---------|-------------|
| `SOLR_URL` | `http://localhost:8983/solr/memories` | Memories collection URL |
| `SOLR_URL_CODE` | `http://localhost:8983/solr/code` | Code collection URL |

Pass `--http :8080` for HTTP mode (recommended for remote access). Default is stdio.

### solr-mem-indexer

| Env var | Default | Description |
|---------|---------|-------------|
| `SOLR_URL_CODE` | `http://localhost:8983/solr/code` | Code collection URL |
| `CLONE_DIR` | `/tmp/solr-mem-repos` | Where to clone/manage repos |
| `INDEX_REPOS` | | Comma-separated repo paths or URLs |
| `INDEX_BRANCH` | `main` | Branch to track |
| `INDEX_INTERVAL` | `300` | Poll interval in seconds (watch mode) |

When `CLONE_DIR` is set, the indexer manages its own clones and can safely `git reset --hard` to track the configured branch. Source repos are never modified.

## Content format guidance

Memories should use compact structured formats for token efficiency:

```yaml
issue: GetOneGSI1 uses strong consistency
file: pkg/db/db.go:685
fix: add WithWeakConsistency()
effort: trivial
risk: very_low
tags: [bug, consistency, gsi]
```

## Deployment

### Docker Compose (Solr)

```bash
make up        # Start Solr
make down      # Stop Solr
make reset     # Stop Solr and delete all data
make logs      # Tail Solr logs
```

### systemd (Linux)

```bash
make systemd-install    # Install and start MCP server
make indexer-install     # Install and start indexer
```

### launchd (macOS)

Full setup from scratch on a Mac with Homebrew:

```bash
# Install prerequisites
brew install go git colima docker docker-compose

# Start container runtime
colima start

# Clone and build
git clone git@github.com:arreyder/solr-mem.git ~/repos/solr-mem
cd ~/repos/solr-mem
make build-all

# Start Solr
make up

# Wait for Solr, then create the code collection
# (memories collection is created automatically by docker-compose)
docker exec solr-mem mkdir -p /var/solr/data/code/conf
docker cp solr/code-managed-schema.xml solr-mem:/var/solr/data/code/conf/managed-schema.xml
docker cp solr/code-solrconfig.xml solr-mem:/var/solr/data/code/conf/solrconfig.xml
docker cp solr/stopwords.txt solr-mem:/var/solr/data/code/conf/stopwords.txt
echo "name=code" | docker exec -i solr-mem tee /var/solr/data/code/core.properties
curl "http://localhost:8983/solr/admin/cores?action=CREATE&name=code&instanceDir=/var/solr/data/code"

# Install launchd services
make launchd-install-server
make launchd-install-indexer INDEX_REPOS=git@github.com:org/repo.git

# Or both at once
make launchd-install INDEX_REPOS=git@github.com:org/repo.git

# Verify
curl http://localhost:8080/healthz
tail -f /tmp/solr-mem-server.log
tail -f /tmp/solr-mem-indexer.log

# Uninstall
make launchd-uninstall
```

The indexer manages its own clones in `~/solr-mem-repos/` and polls for new commits every 5 minutes.

Logs: `/tmp/solr-mem-server.log` and `/tmp/solr-mem-indexer.log`

## Architecture

```
git repos ──> solr-mem-indexer ──> Solr "code" collection
                (Go AST parser)        |
                (two-pass crossref)    |
                                       v
                              solr-mem-server (MCP)
                                       ^
                                       |
              agent memories ──> Solr "memories" collection
```

The Go parser uses `go/ast` from the standard library (no CGO). A heuristic fallback handles Python, TypeScript, JavaScript, Rust, Java, and Ruby. Vendor code is indexed but tagged with lower importance so your own code ranks first in search results.
