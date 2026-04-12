# solr-mem

AI agent memory and code knowledge base backed by Apache Solr. Provides an MCP server with tools for persistent memory storage, pre-analyzed code search across indexed git repositories, and an async memory broker that surfaces relevant context to worker agents at checkpoints.

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
| `solr-mem-server` | MCP server (stdio or HTTP) with 17 tools |
| `solr-mem-indexer` | Git repo indexer with watch mode for incremental updates |
| Solr 9 | Search engine with two collections: `memories` and `code` |

## Quick start

```bash
# Create a .solr-mem.yaml in your source directory
cat > ~/src/.solr-mem.yaml <<'EOF'
repos:
  - path: conductorone/baton/main
  - path: arreyder/solr-mem/main
EOF

# Start Solr + MCP server + index
SRC_DIR=~/src make docker-up

# Add to Claude Code
claude mcp add -s user --transport http solr-mem http://localhost:8080/mcp
```

The indexer scans `SRC_DIR` for `.solr-mem.yaml` files and indexes every repo listed in them. Paths in the config are relative to the file's location. Solr runs on port 8983, the MCP server on port 8080.

### `.solr-mem.yaml`

Place these anywhere in your source tree. The indexer discovers all of them. Paths are relative to the config file's location.

> **Note:** The scanner only descends 4 directories deep from `SRC_DIR` when looking for config files. Place `.solr-mem.yaml` files at the root, org, or repo level — not buried inside source trees. This keeps the scan fast, especially on network-mounted filesystems.

```yaml
repos:
  - path: main              # worktree relative to this file
  - path: feature-branch    # another worktree
  - path: ../other-repo/main
  - path: /absolute/path/to/repo
    branch: develop         # optional, defaults to main
```

In watch mode (`--watch`), the indexer re-scans for config files on each poll tick. Edit a `.solr-mem.yaml` and the changes are picked up automatically.

### Adding a new repo to the index

To index a new repository, add it to a `.solr-mem.yaml` file under your source root. If one doesn't exist near the repo, create one. For example, to add a repo at `~/src/org/new-repo/main`:

```bash
# Option 1: add to an existing .solr-mem.yaml
echo '  - path: org/new-repo/main' >> ~/src/.solr-mem.yaml

# Option 2: create a .solr-mem.yaml next to the repo
cat > ~/src/org/new-repo/.solr-mem.yaml <<'EOF'
repos:
  - path: main
EOF
```

If the indexer is running in watch mode, it picks up the change on the next poll (default: 5 minutes). For immediate indexing:

```bash
# One-shot re-scan
./bin/solr-mem-indexer --scan-dir ~/src --once

# Or with docker
SRC_DIR=~/src docker compose --profile network run --rm indexer
```

### Manual setup (stdio mode)

```bash
# Start Solr only
make up

# Build both binaries
make build-all

# Run MCP server (stdio mode for Claude Code)
SOLR_URL=http://localhost:8983/solr/memories \
SOLR_URL_CODE=http://localhost:8983/solr/code \
  ./bin/solr-mem-server

# Index using .solr-mem.yaml files
./bin/solr-mem-indexer --scan-dir ~/src --once

# Or index a single repo
./bin/solr-mem-indexer --repo /path/to/repo --once

# Watch mode (re-scans configs + polls for new commits every 5 minutes)
./bin/solr-mem-indexer --scan-dir ~/src --watch
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

### Broker tools

The memory broker helps worker agents stay informed without interrupting their flow. Agents report observations as they work; the broker asynchronously searches memories and code for relevant context and prepares a compact packet for pickup at checkpoints.

| Tool | Description |
|------|-------------|
| `observe_work` | Report what the agent is doing. Returns immediately; triggers async packet build. |
| `get_memory_packet` | Retrieve a precomputed packet of relevant memories and code context. |
| `ack_memory_packet` | Acknowledge a packet so the broker can build a fresh one. |

#### observe_work

Accepts a structured work observation:

| Field | Required | Description |
|-------|----------|-------------|
| `run_id` | yes | Unique identifier for this work session |
| `agent_id` | no | ID of the reporting agent |
| `repo` | no | Repository being worked on |
| `phase` | no | Current phase (planning, implementing, debugging, reviewing) |
| `task` | no | What the agent is working on |
| `subgoal` | no | Current immediate subgoal |
| `entities` | no | Key entities: function names, types, packages |
| `code_refs` | no | File paths or symbol references being touched |
| `uncertainty` | no | What the agent is unsure about |
| `next_action` | no | What the agent plans to do next |

Returns immediately with `{status: "accepted", seq, pending}`. The broker queries both Solr collections in the background. If a build is already in progress for this run, the observation is coalesced — the run is marked dirty and a single rebuild happens when the current build finishes.

#### get_memory_packet

Returns the current packet status for a run. The `status` field is always present and takes one of four values:

| Status | Meaning |
|--------|---------|
| `ready` | Packet available. Response includes `packet` with ranked items. |
| `building` | Build in progress. Try again shortly. |
| `acked` | Previous packet was acknowledged. Send a new observation to trigger a fresh build. |
| `none` | Run ID not known. Call `observe_work` first. |

When `status` is `ready`, the response includes a `packet` with:
- `run_id`, `phase`, `delivery` (checkpoint or interrupt)
- `items[]` — up to 5 ranked, deduplicated items with provenance
- `observation_count`, `built_from_seq`, `packet_version`, `generated_at`

Each item includes `source` (memory/code), `source_id`, `title`, `summary`, `relevance` (0–1), `reason` (why included), and optional `tags`, `file_path`, `symbol_name`.

The response always includes `current_seq` — compare with `packet.built_from_seq` to check if the packet reflects your latest observation.

#### ack_memory_packet

Clears the current packet for a run. After ack, `get_memory_packet` returns `status: "acked"` until the next observation triggers a new build.

#### Worker checkpoint pattern

```
1. observe_work(run_id, task, entities, ...)     # report what you're doing
2. ... continue working ...
3. observe_work(run_id, subgoal, code_refs, ...) # report progress (coalesced if build in flight)
4. ... continue working ...
5. get_memory_packet(run_id)                      # at a checkpoint, check for context
   → status: "ready"                              # packet available
   → read packet.items, apply relevant context
6. ack_memory_packet(run_id)                      # clear packet for fresh generation
7. ... continue working, repeat from 1 ...
```

If `get_memory_packet` returns `building`, wait briefly and retry. If it returns `acked` or `none`, send a new `observe_work` to trigger a fresh build.

#### Broker limitations

- **In-memory only.** Observations and packets are not persisted to Solr. Server restart clears all broker state.
- **No LLM summarization.** Scoring is deterministic keyword overlap. No LLM calls.
- **No per-agent isolation.** Partitioned by `run_id` only.
- **Basic interrupt detection.** Delivery is promoted to `interrupt` only on relevance ≥0.9 with exact entity match.
- **Fixed limits.** Max 5 packet items, 30-minute run TTL, 5-minute sweep interval.

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

| Flag / Env var | Default | Description |
|----------------|---------|-------------|
| `--scan-dir` | | Root directory to scan for `.solr-mem.yaml` config files |
| `--repo` | | Path to a single git repository to index |
| `--once` | | Index once and exit |
| `--watch` | | Poll for changes and config file updates |
| `SOLR_URL_CODE` | `http://localhost:8983/solr/code` | Code collection URL |
| `CLONE_DIR` | `/tmp/solr-mem-repos` | Where to clone/manage repos (not used with `--scan-dir`) |
| `INDEX_REPOS` | | Comma-separated repo paths or URLs |
| `INDEX_BRANCH` | `main` | Branch to track |
| `INDEX_INTERVAL` | `300` | Poll interval in seconds (watch mode) |

With `--scan-dir`, the indexer reads repos directly from local paths listed in `.solr-mem.yaml` files. With `CLONE_DIR` + `INDEX_REPOS`, it manages its own clones and can safely `git reset --hard` to track branches. Source repos are never modified in either mode.

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

### Docker Compose

```bash
SRC_DIR=~/src make docker-up  # Start Solr + MCP server + scan & index
make docker-up                # Same, scans current directory
make up                       # Start Solr only
make down                     # Stop all services
make reset                    # Stop all services and delete all data
make logs                     # Tail logs
```

`SRC_DIR` is mounted read-only into the indexer container. It defaults to `.` if not set. The indexer finds all `.solr-mem.yaml` files under it.

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
