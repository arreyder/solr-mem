package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolHandler runs a tool with JSON-like arguments.
type ToolHandler func(context.Context, map[string]any) (any, error)

// ToolDefinition combines a tool's schema with its handler.
type ToolDefinition struct {
	Tool    *mcp.Tool
	Handler ToolHandler
}

// ToolSchemas returns all tool definitions.
func ToolSchemas() []ToolDefinition {
	return []ToolDefinition{
		{
			Tool: &mcp.Tool{
				Name: "store_memory",
				Description: `Store a new memory in the knowledge base.

**When to use**: Save observations, reflections, facts, conversations, tasks, or decisions for later retrieval.

**Required**: content (the memory text)
**Optional**: agent_id, memory_type, title, tags, source, importance, metadata, lifetime, session_id, related_ids, expires_at

**Lifetime values**: permanent (default, never expires), session (cleaned up with session), ephemeral (1 hour TTL), temporary (7 day TTL)

**Content format**: Use compact structured formats (YAML, key-value, tables) over prose. Put searchable summary in title, machine-readable data in metadata (JSON), and categorization in tags. See server instructions for examples.`,
				InputSchema: NewObjectSchema(map[string]any{
					"content":     prop("string", "The main text content of the memory (required)"),
					"agent_id":    prop("string", "ID of the agent storing this memory"),
					"memory_type": prop("string", "Type: observation, reflection, fact, conversation, task, decision"),
					"title":       prop("string", "Short title or summary of the memory"),
					"tags":        arrayPropSchema(prop("string", "Tag"), "Categorization tags"),
					"source":      prop("string", "Where this memory came from (e.g., conversation, tool, file)"),
					"importance":  numberProp("Importance score from 0.0 to 1.0", floatPtr(0), floatPtr(1)),
					"metadata":    prop("string", "JSON string of arbitrary metadata"),
					"lifetime":    prop("string", "Memory lifetime: permanent (default), session, ephemeral (1h), temporary (7d)"),
					"session_id":  prop("string", "Session/conversation ID to group memories"),
					"related_ids": arrayPropSchema(prop("string", "ID"), "IDs of related memories"),
					"expires_at":  prop("string", "Explicit expiration date (ISO 8601). Overrides lifetime."),
					"format":      prop("string", "Content format: yaml, markdown, json, table, prose (default: prose). Helps agents choose parsing strategy."),
				}, "content"),
			},
			Handler: storeMemoryTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "search_memories",
				Description: `Search memories using full-text search with optional filters.

**When to use**: Find relevant memories by content, tags, type, agent, or time range. Uses edismax with field boosting (content^3, title^2, tags^1.5) and recency boost.

**Required**: query (search text)
**Optional**: agent_id, memory_type, tags, source, importance_min, from, to, limit, highlight, facet, session_id, lifetime`,
				InputSchema: NewObjectSchema(map[string]any{
					"query":          prop("string", "Full-text search query (required)"),
					"agent_id":       prop("string", "Filter by agent ID"),
					"memory_type":    prop("string", "Filter by memory type"),
					"tags":           arrayPropSchema(prop("string", "Tag"), "Filter by tags (AND logic)"),
					"source":         prop("string", "Filter by source"),
					"importance_min": numberProp("Minimum importance score", floatPtr(0), floatPtr(1)),
					"from":           prop("string", "Start date filter (ISO 8601)"),
					"to":             prop("string", "End date filter (ISO 8601)"),
					"limit":          integerProp("Max results to return (default: 10, max: 100)", intPtr(1), intPtr(100)),
					"highlight":      prop("boolean", "Include highlighted snippets (default: true)"),
					"facet":          prop("boolean", "Include facet counts (default: false)"),
					"session_id":     prop("string", "Filter by session ID"),
					"lifetime":       prop("string", "Filter by lifetime (permanent, session, ephemeral, temporary)"),
				}, "query"),
			},
			Handler: searchMemoriesTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "update_memory",
				Description: `Update an existing memory by ID using atomic updates.

**When to use**: Modify a memory's content, tags, importance, or other fields. Only specified fields are changed. Changing lifetime recalculates expires_at.

**Required**: id
**Optional**: content, title, memory_type, tags, source, importance, metadata, lifetime, session_id, related_ids, expires_at, format`,
				InputSchema: NewObjectSchema(map[string]any{
					"id":          prop("string", "The memory ID to update (required)"),
					"content":     prop("string", "New content text"),
					"title":       prop("string", "New title"),
					"memory_type": prop("string", "New memory type"),
					"tags":        arrayPropSchema(prop("string", "Tag"), "New tags (replaces existing)"),
					"source":      prop("string", "New source"),
					"importance":  numberProp("New importance score", floatPtr(0), floatPtr(1)),
					"metadata":    prop("string", "New metadata JSON string"),
					"lifetime":    prop("string", "New lifetime (recalculates expires_at)"),
					"session_id":  prop("string", "New session ID"),
					"related_ids": arrayPropSchema(prop("string", "ID"), "New related memory IDs (replaces existing)"),
					"expires_at":  prop("string", "New expiration date (ISO 8601)"),
					"format":      prop("string", "New content format: yaml, markdown, json, table, prose"),
				}, "id"),
			},
			Handler: updateMemoryTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "delete_memory",
				Description: `Delete a memory by ID.

**When to use**: Remove a specific memory that is no longer needed.

**Required**: id`,
				InputSchema: NewObjectSchema(map[string]any{
					"id": prop("string", "The memory ID to delete (required)"),
				}, "id"),
			},
			Handler: deleteMemoryTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "list_memories",
				Description: `List memories with optional filters and facet counts.

**When to use**: Browse stored memories, get an overview of what's stored, or list memories for a specific agent, type, or session.

**Optional**: agent_id, memory_type, source, session_id, lifetime, limit, sort`,
				InputSchema: NewObjectSchema(map[string]any{
					"agent_id":    prop("string", "Filter by agent ID"),
					"memory_type": prop("string", "Filter by memory type"),
					"source":      prop("string", "Filter by source"),
					"session_id":  prop("string", "Filter by session ID"),
					"lifetime":    prop("string", "Filter by lifetime"),
					"limit":       integerProp("Max results (default: 20, max: 100)", intPtr(1), intPtr(100)),
					"sort":        prop("string", "Sort order (default: created_at desc). Examples: importance desc, updated_at desc"),
				}),
			},
			Handler: listMemoriesTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "similar_memories",
				Description: `Find memories similar to a given memory using Solr's MoreLikeThis.

**When to use**: Discover related or duplicate memories based on content similarity. Does not require embeddings — uses term frequency analysis.

**Required**: id (the memory to find similar ones to)
**Optional**: limit (default 5), agent_id filter`,
				InputSchema: NewObjectSchema(map[string]any{
					"id":       prop("string", "The memory ID to find similar memories for (required)"),
					"limit":    integerProp("Max similar results (default: 5, max: 50)", intPtr(1), intPtr(50)),
					"agent_id": prop("string", "Filter similar results by agent ID"),
				}, "id"),
			},
			Handler: similarMemoriesTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "memory_stats",
				Description: `Get statistics about stored memories.

**When to use**: Understand what memories are stored — counts by type, agent, source, lifetime, and date range.

**Optional**: agent_id (scope stats to a specific agent)`,
				InputSchema: NewObjectSchema(map[string]any{
					"agent_id": prop("string", "Scope stats to a specific agent"),
				}),
			},
			Handler: memoryStatsTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "bulk_store_memories",
				Description: `Store multiple memories in a single batch operation.

**When to use**: Efficiently store many memories at once (e.g., importing notes, bulk archival). Each memory in the array uses the same fields as store_memory.

**Required**: memories (array of memory objects, each with at least a "content" field)`,
				InputSchema: NewObjectSchema(map[string]any{
					"memories": arrayPropSchema(
						NewObjectSchema(map[string]any{
							"content":     prop("string", "Memory content (required)"),
							"agent_id":    prop("string", "Agent ID"),
							"memory_type": prop("string", "Memory type"),
							"title":       prop("string", "Title"),
							"tags":        arrayPropSchema(prop("string", "Tag"), "Tags"),
							"source":      prop("string", "Source"),
							"importance":  numberProp("Importance", floatPtr(0), floatPtr(1)),
							"metadata":    prop("string", "Metadata JSON"),
							"lifetime":    prop("string", "Lifetime"),
							"session_id":  prop("string", "Session ID"),
							"related_ids": arrayPropSchema(prop("string", "ID"), "Related IDs"),
							"expires_at":  prop("string", "Expiration (ISO 8601)"),
							"format":      prop("string", "Content format: yaml, markdown, json, table, prose"),
						}, "content"),
						"Array of memory objects to store",
					),
				}, "memories"),
			},
			Handler: bulkStoreMemoriesTool,
		},
		{
			Tool: &mcp.Tool{
				Name: "relate_memories",
				Description: `Link memories together as related.

**When to use**: Create bidirectional relationships between memories. Each memory's related_ids field is updated to include the other IDs.

**Required**: ids (array of 2+ memory IDs to link together)`,
				InputSchema: NewObjectSchema(map[string]any{
					"ids": arrayPropSchema(prop("string", "Memory ID"), "Array of memory IDs to link together (minimum 2)"),
				}, "ids"),
			},
			Handler: relateMemoriesTool,
		},
	}
}
