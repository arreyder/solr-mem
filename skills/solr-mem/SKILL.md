---
name: solr-mem
description: Guidance for using the solr-mem MCP server — persistent memory storage and pre-analyzed code search across indexed git repositories. Triggers when solr-mem tools are available.
disable-model-invocation: false
user-invocable: true
---

# solr-mem MCP Usage Guide

You have access to a solr-mem MCP server with two capabilities: **persistent memory** and **pre-analyzed code search**. This skill teaches you when and how to use each tool effectively.

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
