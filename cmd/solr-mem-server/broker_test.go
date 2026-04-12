package main

import (
	"testing"
	"time"
)

func TestBrokerObserveAndGetPacket(t *testing.T) {
	// Broker with nil clients — skips Solr queries, produces empty packets.
	b := NewBroker(nil, nil)

	obs := WorkObservation{
		RunID:    "run-1",
		AgentID:  "agent-a",
		Phase:    "implementing",
		Task:     "add memory broker",
		Entities: []string{"Broker", "MemoryPacket"},
	}

	pending := b.Observe(obs)
	if pending != 1 {
		t.Errorf("expected 1 pending, got %d", pending)
	}

	// Wait for async goroutine to finish.
	time.Sleep(100 * time.Millisecond)

	pkt := b.GetPacket("run-1", "")
	if pkt == nil {
		t.Fatal("expected a packet, got nil")
	}
	if pkt.RunID != "run-1" {
		t.Errorf("expected run_id=run-1, got %s", pkt.RunID)
	}
	if pkt.Phase != "implementing" {
		t.Errorf("expected phase=implementing, got %s", pkt.Phase)
	}
	if pkt.Delivery != DeliveryCheckpoint {
		t.Errorf("expected delivery=checkpoint, got %s", pkt.Delivery)
	}
	if pkt.ObservationCount != 1 {
		t.Errorf("expected observation_count=1, got %d", pkt.ObservationCount)
	}
}

func TestBrokerPhaseFilter(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{
		RunID: "run-2",
		Phase: "planning",
		Task:  "design broker",
	})

	time.Sleep(100 * time.Millisecond)

	// Requesting a different phase should return nil (not an interrupt).
	pkt := b.GetPacket("run-2", "implementing")
	if pkt != nil {
		t.Errorf("expected nil packet for mismatched phase, got %+v", pkt)
	}

	// Requesting the matching phase should return the packet.
	pkt = b.GetPacket("run-2", "planning")
	if pkt == nil {
		t.Fatal("expected packet for matching phase, got nil")
	}

	// Empty phase should return any packet.
	pkt = b.GetPacket("run-2", "")
	if pkt == nil {
		t.Fatal("expected packet for empty phase filter, got nil")
	}
}

func TestBrokerAckPacket(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{
		RunID: "run-3",
		Phase: "debugging",
		Task:  "fix tests",
	})

	time.Sleep(100 * time.Millisecond)

	// Packet should exist.
	pkt := b.GetPacket("run-3", "")
	if pkt == nil {
		t.Fatal("expected a packet before ack")
	}

	// Ack it.
	acked := b.AckPacket("run-3")
	if !acked {
		t.Error("expected ack to return true")
	}

	// Should be gone now.
	pkt = b.GetPacket("run-3", "")
	if pkt != nil {
		t.Error("expected nil packet after ack")
	}

	// Double ack returns false.
	acked = b.AckPacket("run-3")
	if acked {
		t.Error("expected second ack to return false")
	}
}

func TestBrokerUnknownRun(t *testing.T) {
	b := NewBroker(nil, nil)

	pkt := b.GetPacket("nonexistent", "")
	if pkt != nil {
		t.Error("expected nil for unknown run")
	}

	acked := b.AckPacket("nonexistent")
	if acked {
		t.Error("expected false ack for unknown run")
	}
}

func TestBrokerMultipleObservations(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{
		RunID: "run-4",
		Phase: "planning",
		Task:  "first task",
	})
	time.Sleep(100 * time.Millisecond)

	pending := b.Observe(WorkObservation{
		RunID: "run-4",
		Phase: "implementing",
		Task:  "second task",
	})
	if pending != 2 {
		t.Errorf("expected 2 pending, got %d", pending)
	}

	time.Sleep(100 * time.Millisecond)

	pkt := b.GetPacket("run-4", "")
	if pkt == nil {
		t.Fatal("expected a packet after multiple observations")
	}
	if pkt.ObservationCount != 2 {
		t.Errorf("expected observation_count=2, got %d", pkt.ObservationCount)
	}
}

func TestBuildQueryTerms(t *testing.T) {
	obs := WorkObservation{
		Task:     "add memory broker",
		Subgoal:  "implement observe_work",
		Entities: []string{"Broker", "MemoryPacket"},
	}
	q := buildQueryTerms(obs)
	if q == "" {
		t.Error("expected non-empty query terms")
	}
	for _, expected := range []string{"add memory broker", "implement observe_work", "Broker", "MemoryPacket"} {
		if !contains(q, expected) {
			t.Errorf("expected query to contain %q, got %q", expected, q)
		}
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"a b", nil}, // all too short
		{"hello world", []string{"hello", "world"}},
		{"Broker.Observe()", []string{"broker.observe()"}},
		{"file.go:123", []string{"file.go:123"}},
	}

	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("tokenize(%q): expected %v, got %v", tt.input, tt.expected, got)
		}
	}
}

func TestScoreCandidatesDedup(t *testing.T) {
	candidates := []MemoryPacketItem{
		{SourceID: "id-1", Title: "broker", Summary: "memory broker"},
		{SourceID: "id-1", Title: "broker dup", Summary: "duplicate"},
		{SourceID: "id-2", Title: "other", Summary: "different item"},
	}
	obs := WorkObservation{Task: "memory broker"}

	scored := scoreCandidates(candidates, obs)
	if len(scored) != 2 {
		t.Errorf("expected 2 deduped items, got %d", len(scored))
	}

	// Check sorted by relevance descending.
	for i := 1; i < len(scored); i++ {
		if scored[i].Relevance > scored[i-1].Relevance {
			t.Error("expected sorted by relevance descending")
		}
	}
}

func TestComputeRelevance(t *testing.T) {
	item := MemoryPacketItem{
		Title:      "memory broker implementation",
		Summary:    "broker that handles work observations",
		SymbolName: "Broker",
	}
	obs := WorkObservation{
		Task:     "memory broker",
		Entities: []string{"Broker"},
	}

	score := computeRelevance(item, obs)
	if score < 0.5 {
		t.Errorf("expected relevance >= 0.5 for matching item, got %.2f", score)
	}
	if score > 1.0 {
		t.Errorf("relevance should not exceed 1.0, got %.2f", score)
	}
}

func TestFormatPacket(t *testing.T) {
	pkt := &MemoryPacket{
		RunID:            "run-test",
		Phase:            "testing",
		Delivery:         DeliveryCheckpoint,
		Summary:          "Found 1 memories relevant to testing",
		ObservationCount: 3,
		GeneratedAt:      time.Now(),
		Items: []MemoryPacketItem{
			{
				Source:    "memory",
				SourceID:  "mem-1",
				Title:     "test memory",
				Summary:   "some relevant content",
				Relevance: 0.75,
				Reason:    "memory search hit",
			},
		},
	}

	text := formatPacket(pkt)
	if text == "" {
		t.Error("expected non-empty formatted packet")
	}
	if !contains(text, "checkpoint") {
		t.Error("expected 'checkpoint' in formatted output")
	}
	if !contains(text, "run-test") {
		t.Error("expected run_id in formatted output")
	}
	if !contains(text, "test memory") {
		t.Error("expected item title in formatted output")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
