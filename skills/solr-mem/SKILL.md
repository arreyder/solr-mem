---
name: solr-mem
description: Guidance for using the solr-mem MCP server — persistent memory storage, pre-analyzed code search, and async memory broker for worker agents. Triggers when solr-mem tools are available.
disable-model-invocation: false
user-invocable: true
---

# solr-mem MCP Usage Guide

You have access to a solr-mem MCP server with three capabilities: **persistent memory**, **pre-analyzed code search**, and an **async memory broker**. This skill teaches you when and how to use each tool effectively.

---

## Code Search

Indexed repositories are pre-analyzed into a four-level document hierarchy. Each level contains structured YAML summaries — not raw source. This is dramatically cheaper than reading files.

### Document Hierarchy

| Level | What it contains | When to use |
|-------|-----------------|-------------|
| **repo** | Languages, structure, stats | Orient yourself in an unfamiliar repo |
| **package** | Key types, entry points, interfaces, implementations | Understand what a package does |
| **file** | Grouped symbols, categorized imports, interface hints | Find where something lives |
| **symbol** | Params, returns, calls, types_used, called_by, source | Understand a specific function/type |

### Tool Selection

**`browse_code`** — Navigate the hierarchy top-down.
- Start here when exploring an unfamiliar repo
- List all indexed repos (no args), then drill: repo > packages > files > symbols
- Use `parent_id` to list children of any document
- Use `doc_level` to filter (e.g., show only packages in a repo)

**`search_code`** — Full-text search across all indexed repos.
- Use when you know *what* you're looking for but not *where*
- Symbol names are boosted 4x, so searching a function name works well
- Filter with `doc_level`, `symbol_type`, `repo_url`, `language`, `file_path`
- Use `doc_level: "package"` to find which package owns a concept
- Use `symbol_type: "interface"` to find interfaces

**`get_symbol`** — Fetch a specific symbol by exact name.
- Use when you already know the function/type name
- Returns full source code + metadata
- Set `include_related: true` to also get the parent file doc and sibling symbols

**`code_context`** — Get context for a file location.
- Use when you have a file path (and optionally a line number)
- Returns file summary + enclosing symbol + neighbors
- Great for "what's going on at this location?"

**`code_context_bundle`** — Complete working set for a symbol.
- **This is the power tool.** Use it when you need to understand or modify a function.
- Returns: symbol YAML + source + all callees + all callers + types used + package context
- One call replaces what would otherwise be 10+ file reads
- Use `depth: 2` for transitive call graphs (callers of callers)

### Code Search Patterns

**Understanding a function before modifying it:**
```
code_context_bundle(symbol_name: "IndexRepo")
```
This gives you the function, everything it calls, everything that calls it, and the types involved.

**Finding where a concept lives:**
```
search_code(query: "authentication middleware", doc_level: "package")
```
Package-level docs summarize purpose, so this finds the right package fast.

**Exploring a new repo:**
```
browse_code()                                    # list all repos
browse_code(repo_url: "ductone/c1")             # list packages
browse_code(repo_url: "ductone/c1", doc_level: "package", parent_id: "...")  # drill in
```

**Finding all implementations of an interface:**
```
search_code(query: "InterfaceName", symbol_type: "struct")
```
Struct docs include `implements` fields from cross-reference analysis.

**Getting file-level overview:**
```
code_context(file_path: "pkg/auth/middleware.go")
```

### Code Search Anti-Patterns

- Do NOT read raw source files when the repo is indexed — use code tools instead (94x fewer tokens)
- Do NOT use `search_code` for broad exploration — use `browse_code` to navigate the hierarchy
- Do NOT call `get_symbol` then separately search for callers — use `code_context_bundle` which includes both
- Do NOT search without filters on large repos — use `repo_url`, `doc_level`, or `symbol_type` to narrow results

---

## Memory Storage

Memories persist across conversations. Use them to accumulate knowledge that future sessions need.

### When to Store Memories

- Findings from investigation that would be expensive to re-derive
- Decisions and their rationale (why approach X was chosen over Y)
- User preferences and project context discovered during work
- Cross-cutting observations (patterns, risks, debt) that span files
- Architecture insights not obvious from reading code

### When NOT to Store Memories

- Information derivable from code or git history (use code search instead)
- Ephemeral task state (use session lifetime or don't store)
- Raw data dumps — distill to the insight

### Content Format

Always use compact structured formats. YAML is preferred over prose:

```yaml
# Good — structured, searchable, compact
title: "auth middleware session token compliance issue"
content: |
  issue: session tokens stored in non-compliant format
  location: pkg/auth/middleware.go
  impact: legal/compliance blocker
  decision: full rewrite, not patch
  owner: platform team
tags: [auth, compliance, architecture-decision]
```

```
# Bad — verbose prose, poor searchability
"The authentication middleware has an issue where session tokens
are stored in a way that doesn't meet compliance requirements..."
```

### Tool Selection

**`store_memory`** — Save a single memory.
- Put the most searchable summary in `title` (boosted 2x in search)
- Use `tags` liberally (boosted 1.5x, filterable)
- Use `metadata` for machine-readable JSON (file paths, line numbers, metrics)
- Set `memory_type`: observation, reflection, fact, conversation, task, decision
- Set `importance` (0.0-1.0) to help prioritize search results
- Use `format: "yaml"` when content is YAML

**`search_memories`** — Find relevant memories.
- Primary retrieval tool — uses edismax with field boosting and recency boost
- Filter by `memory_type`, `tags`, `agent_id`, `importance_min`, date range
- Always search before storing to avoid duplicates

**`bulk_store_memories`** — Batch insert multiple memories.
- Use when storing 3+ memories at once (e.g., after an investigation)
- Same fields as `store_memory` per item

**`similar_memories`** — Find related memories by content similarity.
- Uses term frequency analysis, not embeddings
- Good for deduplication checks and finding related context

**`relate_memories`** — Link memories bidirectionally.
- Use after storing related findings to create navigable clusters
- Pass array of 2+ memory IDs

**`update_memory`** — Modify existing memories.
- Only specified fields are changed (atomic update)
- Use to refine, correct, or reclassify memories over time

**`list_memories`** — Browse all memories.
- Good for overview and cleanup
- Sort by `importance desc`, `created_at desc`, or `updated_at desc`

### Lifetime Guide

| Lifetime | TTL | Use for |
|----------|-----|---------|
| `permanent` | Never expires | Architecture decisions, project context, user preferences |
| `temporary` | 7 days | Sprint-scoped findings, in-progress investigations |
| `ephemeral` | 1 hour | Scratch notes, intermediate results |
| `session` | Session end | Conversation-local context |

Default is `permanent`. Use shorter lifetimes for things that will become stale.

### Memory Patterns

**After investigating a bug:**
```
store_memory(
  title: "GetOneGSI1 strong consistency bug",
  content: "issue: GetOneGSI1 uses strong consistency...",
  tags: ["bug", "consistency", "gsi", "database"],
  memory_type: "observation",
  format: "yaml",
  importance: 0.8
)
```

**Before storing, check for duplicates:**
```
search_memories(query: "GSI1 consistency")
```

**Link related findings:**
```
relate_memories(ids: ["mem-abc", "mem-def", "mem-ghi"])
```

---

## Async Memory Broker

The broker is for **long-running work sessions** where you want relevant context surfaced automatically without stopping to search. You report what you're doing; the broker searches memories and code in the background and prepares a small packet for you to pick up at checkpoints.

### When to Use the Broker

- Multi-step implementation tasks where the focus area shifts over time
- Debugging sessions where each step narrows the problem
- Any work where relevant memories or code context might exist but you don't want to pause to search

### When NOT to Use the Broker

- Quick one-shot questions — just use `search_memories` or `search_code` directly
- When you already know exactly which symbol or file you need — use `code_context_bundle` or `get_symbol`
- For storing findings — use `store_memory` or `bulk_store_memories` directly

### Tool Selection

**`observe_work`** — Report what you're doing.
- Call at natural transitions: starting a subtask, shifting focus, encountering uncertainty
- Include `entities` (function/type names) and `code_refs` (file paths) — these drive the code search
- Include `uncertainty` when you're unsure about something — the broker searches for related context
- Returns immediately with `{status: "accepted", seq, pending}`
- If a build is already in progress, your observation is coalesced — no redundant work

**`get_memory_packet`** — Check for a precomputed context packet.
- Call at checkpoints: phase transitions, before commits, when stuck
- Returns a `status` field — act on it deterministically:

| Status | What it means | What to do |
|--------|--------------|------------|
| `ready` | Packet available with ranked items | Read items, apply relevant context, then ack |
| `building` | Build in progress | Wait briefly, retry |
| `acked` | Previous packet was consumed | Send a new `observe_work` if you want a fresh one |
| `none` | Run not known | Call `observe_work` first |

- When `ready`, the `packet` contains up to 5 items, each with `source` (memory/code), `source_id`, `title`, `summary`, `relevance` (0–1), and `reason` (why included)
- Compare `packet.built_from_seq` with `current_seq` to check if the packet reflects your latest observation

**`ack_memory_packet`** — Clear the current packet.
- Call after you've consumed a packet's items
- Allows the broker to build a fresh packet from newer observations
- If you don't ack, `get_memory_packet` keeps returning the same packet

### Worker Pattern

```
# Starting a task
observe_work(run_id: "run-123", task: "fix auth timeout", entities: ["AuthMiddleware"], phase: "debugging")

# ... investigate, read code, make changes ...

# Focus shifts — report updated context
observe_work(run_id: "run-123", subgoal: "check session expiry logic", code_refs: ["pkg/auth/session.go"], uncertainty: "not sure if TTL is configurable")

# ... continue working ...

# At a checkpoint — check for relevant context
get_memory_packet(run_id: "run-123")
# → status: "ready"
# → packet.items: [{source: "memory", title: "auth session TTL is hardcoded...", reason: "memory search hit"}, ...]

# Apply what's useful, then clear
ack_memory_packet(run_id: "run-123")

# Continue working, repeat
```

### Broker Anti-Patterns

- **Don't spam `observe_work` on every tool call.** Call it at meaningful transitions — new subtask, new file, new uncertainty. A few times per work phase is enough.
- **Don't poll `get_memory_packet` in a tight loop.** Check at natural checkpoints. The build takes a moment; constant polling wastes calls.
- **Don't ack before reading the packet.** The ack clears the packet — read the items first, then ack.
- **Don't treat broker packets as a replacement for precise code tools.** The broker surfaces *possibly relevant* context. When you need a specific symbol's call graph, use `code_context_bundle` directly. The broker is for serendipitous discovery, not targeted lookup.
- **Don't ignore the `status` field.** Always check it before trying to read `packet`. A `building` response is not an error — just retry shortly.

### Freshness and Metadata

Each packet includes metadata for debugging and freshness:
- `built_from_seq` — which observation sequence the packet was built from
- `packet_version` — increments each time a new packet is generated for the run
- `generated_at` — when the build finished
- `observation_count` — total observations in this run
- `current_seq` (on the response, not the packet) — the latest observation seq

If `current_seq > packet.built_from_seq`, the packet was built before your latest observation. A rebuild may be in progress, or you can send another `observe_work` to trigger one.
