package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arreyder/solr-mem/internal/solr"
)

// DeliveryClass indicates the urgency of a memory packet.
type DeliveryClass string

const (
	DeliveryCheckpoint DeliveryClass = "checkpoint"
	DeliveryInterrupt  DeliveryClass = "interrupt"
)

// WorkObservation is a single structured report from a worker agent.
type WorkObservation struct {
	RunID       string   `json:"run_id"`
	AgentID     string   `json:"agent_id,omitempty"`
	Repo        string   `json:"repo,omitempty"`
	Phase       string   `json:"phase,omitempty"`
	Task        string   `json:"task,omitempty"`
	Subgoal     string   `json:"subgoal,omitempty"`
	Entities    []string `json:"entities,omitempty"`
	CodeRefs    []string `json:"code_refs,omitempty"`
	Uncertainty string   `json:"uncertainty,omitempty"`
	NextAction  string   `json:"next_action,omitempty"`
	ReceivedAt  time.Time `json:"received_at"`
}

// MemoryPacketItem is one item in a memory packet with provenance.
type MemoryPacketItem struct {
	Source     string  `json:"source"`      // "memory" or "code"
	SourceID   string  `json:"source_id"`   // Solr document ID
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	Relevance  float64 `json:"relevance"`   // 0-1 combined score
	Reason     string  `json:"reason"`      // why this item was included
	Tags       []string `json:"tags,omitempty"`
	MemoryType string  `json:"memory_type,omitempty"`
	FilePath   string  `json:"file_path,omitempty"`
	SymbolName string  `json:"symbol_name,omitempty"`
}

// MemoryPacket is a precomputed bundle of relevant context for a worker agent.
type MemoryPacket struct {
	RunID            string            `json:"run_id"`
	Phase            string            `json:"phase"`
	Delivery         DeliveryClass     `json:"delivery"`
	Summary          string            `json:"summary"`
	Items            []MemoryPacketItem `json:"items"`
	ObservationCount int               `json:"observation_count"`
	GeneratedAt      time.Time         `json:"generated_at"`
}

// runState tracks the latest observation and built packet for a run.
type runState struct {
	observations []WorkObservation
	packet       *MemoryPacket
	building     bool // true while async gather is in progress
}

// Broker accumulates work observations and builds memory packets.
type Broker struct {
	mu   sync.Mutex
	runs map[string]*runState

	memClient  *solr.Client
	codeClient *solr.Client
}

// NewBroker creates a broker that queries existing Solr collections.
func NewBroker(memClient, codeClient *solr.Client) *Broker {
	return &Broker{
		runs:       make(map[string]*runState),
		memClient:  memClient,
		codeClient: codeClient,
	}
}

// Observe records a work observation and kicks off async packet building.
func (b *Broker) Observe(obs WorkObservation) int {
	obs.ReceivedAt = time.Now().UTC()

	b.mu.Lock()
	rs, ok := b.runs[obs.RunID]
	if !ok {
		rs = &runState{}
		b.runs[obs.RunID] = rs
	}
	rs.observations = append(rs.observations, obs)
	pending := len(rs.observations)

	// Only start building if not already in progress.
	if !rs.building {
		rs.building = true
		// Snapshot the latest observation for the goroutine.
		latest := obs
		b.mu.Unlock()
		go b.buildPacket(latest)
	} else {
		b.mu.Unlock()
	}

	return pending
}

// GetPacket returns the most recent packet for a run, or nil if none is ready.
func (b *Broker) GetPacket(runID, phase string) *MemoryPacket {
	b.mu.Lock()
	defer b.mu.Unlock()

	rs, ok := b.runs[runID]
	if !ok {
		return nil
	}
	pkt := rs.packet
	if pkt == nil {
		return nil
	}
	// If caller specified a phase, only return if it matches or packet is interrupt.
	if phase != "" && pkt.Phase != phase && pkt.Delivery != DeliveryInterrupt {
		return nil
	}
	return pkt
}

// AckPacket clears the current packet for a run so the next get returns nil
// until a new packet is built.
func (b *Broker) AckPacket(runID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	rs, ok := b.runs[runID]
	if !ok {
		return false
	}
	if rs.packet == nil {
		return false
	}
	rs.packet = nil
	return true
}

// buildPacket runs in a goroutine. It queries Solr for relevant memories and
// code, scores/dedupes the results, and stashes a packet.
func (b *Broker) buildPacket(obs WorkObservation) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var candidates []MemoryPacketItem

	// Build a search query from the observation's key fields.
	queryTerms := buildQueryTerms(obs)

	// 1. Search memories collection.
	if len(queryTerms) > 0 && b.memClient != nil {
		memItems := b.searchMemories(ctx, queryTerms, obs)
		candidates = append(candidates, memItems...)
	}

	// 2. Search code collection for referenced entities/files.
	if b.codeClient != nil {
		codeItems := b.searchCode(ctx, obs)
		candidates = append(candidates, codeItems...)
	}

	// 3. Score and dedupe.
	scored := scoreCandidates(candidates, obs)

	// 4. Pick top items (max 5).
	const maxItems = 5
	if len(scored) > maxItems {
		scored = scored[:maxItems]
	}

	// 5. Determine delivery class.
	delivery := DeliveryCheckpoint
	for _, item := range scored {
		// Promote to interrupt if we found a high-relevance hazard or prior solution.
		if item.Relevance >= 0.9 && (strings.Contains(item.Reason, "exact match") || strings.Contains(item.Reason, "hazard")) {
			delivery = DeliveryInterrupt
			break
		}
	}

	// 6. Build summary.
	summary := buildPacketSummary(scored, obs)

	pkt := &MemoryPacket{
		RunID:            obs.RunID,
		Phase:            obs.Phase,
		Delivery:         delivery,
		Summary:          summary,
		Items:            scored,
		ObservationCount: 0, // filled below under lock
		GeneratedAt:      time.Now().UTC(),
	}

	b.mu.Lock()
	rs, ok := b.runs[obs.RunID]
	if ok {
		pkt.ObservationCount = len(rs.observations)
		rs.packet = pkt
		rs.building = false
	}
	b.mu.Unlock()

	log.Printf("broker: built packet for run %s: %d items, delivery=%s",
		obs.RunID, len(scored), delivery)
}

// searchMemories queries the memories collection for items relevant to the observation.
func (b *Broker) searchMemories(ctx context.Context, queryTerms string, obs WorkObservation) []MemoryPacketItem {
	params := solr.QueryParams{
		Query:     queryTerms,
		Rows:      10,
		Highlight: false,
	}
	if obs.AgentID != "" {
		// Include memories from any agent, but we could filter later.
	}

	resp, err := b.memClient.Query(ctx, params)
	if err != nil {
		log.Printf("broker: memory search error: %v", err)
		return nil
	}

	var items []MemoryPacketItem
	for _, doc := range resp.Docs {
		id, _ := doc["id"].(string)
		title, _ := doc["title"].(string)
		content, _ := doc["content"].(string)
		memType, _ := doc["memory_type"].(string)
		tags := getStringSliceFromDoc(doc, "tags")

		// Truncate content for summary.
		summary := content
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}

		items = append(items, MemoryPacketItem{
			Source:     "memory",
			SourceID:   id,
			Title:      title,
			Summary:    summary,
			Relevance:  0.5, // base; refined in scoreCandidates
			Reason:     "memory search hit",
			Tags:       tags,
			MemoryType: memType,
		})
	}
	return items
}

// searchCode queries the code collection for entities and code_refs from the observation.
func (b *Broker) searchCode(ctx context.Context, obs WorkObservation) []MemoryPacketItem {
	var items []MemoryPacketItem

	// Search for explicitly referenced symbols/files.
	searchTerms := make([]string, 0, len(obs.Entities)+len(obs.CodeRefs))
	searchTerms = append(searchTerms, obs.Entities...)
	searchTerms = append(searchTerms, obs.CodeRefs...)

	if len(searchTerms) == 0 {
		return nil
	}

	// Search by symbol name for entities.
	for _, entity := range obs.Entities {
		if entity == "" {
			continue
		}
		params := solr.QueryParams{
			Query:         fmt.Sprintf("symbol_name_exact:%q OR symbol_name:%q", entity, entity),
			FilterQueries: []string{`doc_level:"symbol"`},
			Rows:          3,
			Highlight:     false,
		}
		if obs.Repo != "" {
			params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("repo_url:%q", obs.Repo))
		}

		resp, err := b.codeClient.Query(ctx, params)
		if err != nil {
			log.Printf("broker: code search error for %q: %v", entity, err)
			continue
		}
		for _, doc := range resp.Docs {
			items = append(items, codeDocToItem(doc, fmt.Sprintf("entity match: %s", entity)))
		}
	}

	// Search by file path for code_refs.
	for _, ref := range obs.CodeRefs {
		if ref == "" {
			continue
		}
		params := solr.QueryParams{
			Query:         fmt.Sprintf("file_path:%q", ref),
			FilterQueries: []string{`doc_level:"file" OR doc_level:"symbol"`},
			Rows:          3,
			Highlight:     false,
		}
		if obs.Repo != "" {
			params.FilterQueries = append(params.FilterQueries, fmt.Sprintf("repo_url:%q", obs.Repo))
		}

		resp, err := b.codeClient.Query(ctx, params)
		if err != nil {
			log.Printf("broker: code ref search error for %q: %v", ref, err)
			continue
		}
		for _, doc := range resp.Docs {
			items = append(items, codeDocToItem(doc, fmt.Sprintf("code ref: %s", ref)))
		}
	}

	return items
}

// codeDocToItem converts a Solr code document to a MemoryPacketItem.
func codeDocToItem(doc map[string]any, reason string) MemoryPacketItem {
	id, _ := doc["id"].(string)
	title, _ := doc["title"].(string)
	content, _ := doc["content"].(string)
	filePath, _ := doc["file_path"].(string)
	symbolName, _ := doc["symbol_name"].(string)
	tags := getStringSliceFromDoc(doc, "tags")

	summary := content
	if len(summary) > 200 {
		summary = summary[:200] + "..."
	}

	return MemoryPacketItem{
		Source:     "code",
		SourceID:   id,
		Title:      title,
		Summary:    summary,
		Relevance:  0.5,
		Reason:     reason,
		Tags:       tags,
		FilePath:   filePath,
		SymbolName: symbolName,
	}
}

// buildQueryTerms constructs a Solr query string from observation fields.
func buildQueryTerms(obs WorkObservation) string {
	var parts []string
	if obs.Task != "" {
		parts = append(parts, obs.Task)
	}
	if obs.Subgoal != "" {
		parts = append(parts, obs.Subgoal)
	}
	if obs.Uncertainty != "" {
		parts = append(parts, obs.Uncertainty)
	}
	for _, e := range obs.Entities {
		if e != "" {
			parts = append(parts, e)
		}
	}
	return strings.Join(parts, " ")
}

// scoreCandidates assigns relevance scores and deduplicates by source_id.
func scoreCandidates(candidates []MemoryPacketItem, obs WorkObservation) []MemoryPacketItem {
	if len(candidates) == 0 {
		return nil
	}

	// Dedupe by source_id, keeping first occurrence (which tends to be higher ranked by Solr).
	seen := make(map[string]bool)
	var deduped []MemoryPacketItem
	for _, c := range candidates {
		if seen[c.SourceID] {
			continue
		}
		seen[c.SourceID] = true

		// Score based on keyword overlap with observation.
		c.Relevance = computeRelevance(c, obs)
		deduped = append(deduped, c)
	}

	// Sort by relevance descending.
	sort.Slice(deduped, func(i, j int) bool {
		return deduped[i].Relevance > deduped[j].Relevance
	})

	return deduped
}

// computeRelevance produces a 0-1 score based on keyword overlap and source type.
func computeRelevance(item MemoryPacketItem, obs WorkObservation) float64 {
	score := 0.3 // base

	// Build a set of observation keywords.
	obsWords := make(map[string]bool)
	for _, w := range tokenize(obs.Task) {
		obsWords[w] = true
	}
	for _, w := range tokenize(obs.Subgoal) {
		obsWords[w] = true
	}
	for _, e := range obs.Entities {
		for _, w := range tokenize(e) {
			obsWords[w] = true
		}
	}
	for _, r := range obs.CodeRefs {
		for _, w := range tokenize(r) {
			obsWords[w] = true
		}
	}

	if len(obsWords) == 0 {
		return score
	}

	// Count how many observation keywords appear in the item's title + summary.
	itemText := strings.ToLower(item.Title + " " + item.Summary)
	matches := 0
	for w := range obsWords {
		if strings.Contains(itemText, w) {
			matches++
		}
	}
	overlap := float64(matches) / float64(len(obsWords))
	score += overlap * 0.5

	// Exact entity match in symbol name bumps score.
	if item.SymbolName != "" {
		for _, e := range obs.Entities {
			if strings.EqualFold(item.SymbolName, e) {
				score += 0.2
				item.Reason = "exact match: " + e
				break
			}
		}
	}

	// Cap at 1.0.
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// tokenize splits a string into lowercase words, filtering short ones.
func tokenize(s string) []string {
	if s == "" {
		return nil
	}
	words := strings.Fields(strings.ToLower(s))
	var out []string
	for _, w := range words {
		// Strip common punctuation.
		w = strings.Trim(w, ".,;:!?()[]{}\"'`")
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// buildPacketSummary generates a one-line summary of the packet.
func buildPacketSummary(items []MemoryPacketItem, obs WorkObservation) string {
	if len(items) == 0 {
		return fmt.Sprintf("No relevant context found for phase %q", obs.Phase)
	}

	memCount := 0
	codeCount := 0
	for _, item := range items {
		switch item.Source {
		case "memory":
			memCount++
		case "code":
			codeCount++
		}
	}

	parts := []string{}
	if memCount > 0 {
		parts = append(parts, fmt.Sprintf("%d memories", memCount))
	}
	if codeCount > 0 {
		parts = append(parts, fmt.Sprintf("%d code refs", codeCount))
	}
	return fmt.Sprintf("Found %s relevant to %q", strings.Join(parts, " and "), obs.Task)
}
