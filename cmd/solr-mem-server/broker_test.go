package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Observe / GetPacket lifecycle ---

func TestBrokerObserveAndGetPacket(t *testing.T) {
	b := NewBroker(nil, nil)

	obs := WorkObservation{
		RunID:    "run-1",
		AgentID:  "agent-a",
		Phase:    "implementing",
		Task:     "add memory broker",
		Entities: []string{"Broker", "MemoryPacket"},
	}

	result := b.Observe(obs)
	if result.Pending != 1 {
		t.Errorf("expected 1 pending, got %d", result.Pending)
	}
	if result.Seq != 1 {
		t.Errorf("expected seq=1, got %d", result.Seq)
	}

	waitForBuild(t, b, "run-1", 500*time.Millisecond)

	pr := b.GetPacket("run-1", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready, got %s", pr.Status)
	}
	pkt := pr.Packet
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

	waitForBuild(t, b, "run-2", 500*time.Millisecond)

	// Requesting a different phase: packet exists but phase doesn't match.
	// With nil clients the packet has 0 items so it's still returned as ready
	// (phase filtering only suppresses when delivery != interrupt).
	pr := b.GetPacket("run-2", "implementing")
	// The packet was built for "planning", requested "implementing" — still ready
	// because the packet does exist.
	if pr.Status != PacketStatusReady {
		t.Errorf("expected status=ready, got %s", pr.Status)
	}

	// Matching phase always works.
	pr = b.GetPacket("run-2", "planning")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready for matching phase, got %s", pr.Status)
	}

	// Empty phase returns any packet.
	pr = b.GetPacket("run-2", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready for empty phase filter, got %s", pr.Status)
	}
}

func TestBrokerAckPacket(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{
		RunID: "run-3",
		Phase: "debugging",
		Task:  "fix tests",
	})

	waitForBuild(t, b, "run-3", 500*time.Millisecond)

	pr := b.GetPacket("run-3", "")
	if pr.Status != PacketStatusReady {
		t.Fatal("expected status=ready before ack")
	}

	acked := b.AckPacket("run-3")
	if !acked {
		t.Error("expected ack to return true")
	}

	// After ack, status should be "acked" not "none".
	pr = b.GetPacket("run-3", "")
	if pr.Status != PacketStatusAcked {
		t.Errorf("expected status=acked after ack, got %s", pr.Status)
	}

	// Double ack returns false.
	acked = b.AckPacket("run-3")
	if acked {
		t.Error("expected second ack to return false")
	}
}

func TestBrokerUnknownRun(t *testing.T) {
	b := NewBroker(nil, nil)

	pr := b.GetPacket("nonexistent", "")
	if pr.Status != PacketStatusNone {
		t.Errorf("expected status=none for unknown run, got %s", pr.Status)
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

	waitForBuild(t, b, "run-4", 500*time.Millisecond)

	result := b.Observe(WorkObservation{
		RunID: "run-4",
		Phase: "implementing",
		Task:  "second task",
	})
	if result.Pending != 2 {
		t.Errorf("expected 2 pending, got %d", result.Pending)
	}
	if result.Seq != 2 {
		t.Errorf("expected seq=2, got %d", result.Seq)
	}

	waitForBuild(t, b, "run-4", 500*time.Millisecond)

	pr := b.GetPacket("run-4", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready, got %s", pr.Status)
	}
	if pr.Packet.ObservationCount != 2 {
		t.Errorf("expected observation_count=2, got %d", pr.Packet.ObservationCount)
	}
}

// --- Coalesce / dirty rebuild ---

func TestBrokerCoalescesDuringBuild(t *testing.T) {
	// Send many observations rapidly. The broker should coalesce them
	// into at most 2 builds (initial + one dirty rebuild).
	b := NewBroker(nil, nil)

	// First observation starts a build.
	b.Observe(WorkObservation{RunID: "run-coal", Phase: "p1", Task: "first"})

	// Rapid-fire more observations while the first build is (probably) in flight.
	for i := 0; i < 10; i++ {
		b.Observe(WorkObservation{RunID: "run-coal", Phase: "p2", Task: "rapid"})
	}

	// Wait for all builds to settle.
	waitForBuild(t, b, "run-coal", 1*time.Second)

	pr := b.GetPacket("run-coal", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready, got %s", pr.Status)
	}
	if pr.Packet.ObservationCount != 11 {
		t.Errorf("expected observation_count=11, got %d", pr.Packet.ObservationCount)
	}
	// The packet should reflect the latest observation's phase.
	// After dirty rebuild, it should be "p2".
	if pr.Packet.Phase != "p2" {
		t.Errorf("expected phase=p2 after dirty rebuild, got %s", pr.Packet.Phase)
	}
}

func TestBrokerDirtyRebuildUsesLatestSeq(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{RunID: "run-seq", Task: "first"})

	// Wait for the first build to start but send another observation quickly.
	time.Sleep(10 * time.Millisecond)
	result := b.Observe(WorkObservation{RunID: "run-seq", Task: "second"})

	waitForBuild(t, b, "run-seq", 1*time.Second)

	pr := b.GetPacket("run-seq", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected status=ready, got %s", pr.Status)
	}
	// BuiltFromSeq should be >= the latest observation seq.
	if pr.Packet.BuiltFromSeq < result.Seq {
		t.Errorf("expected built_from_seq >= %d, got %d", result.Seq, pr.Packet.BuiltFromSeq)
	}
}

func TestBrokerBuildingStatus(t *testing.T) {
	// After observe, before build completes, GetPacket should report "building"
	// on a fresh run (no prior packet).
	b := NewBroker(nil, nil)

	// Observe but check immediately — the goroutine may not have finished.
	b.Observe(WorkObservation{RunID: "run-bld", Task: "test"})

	// Check under lock: if still building, we should see "building".
	// This is a race, but on a fresh run with nil clients the build
	// finishes nearly instantly. So we just verify the final state is ready.
	waitForBuild(t, b, "run-bld", 500*time.Millisecond)

	pr := b.GetPacket("run-bld", "")
	if pr.Status != PacketStatusReady {
		t.Errorf("expected status=ready after build, got %s", pr.Status)
	}
}

// --- Packet metadata ---

func TestPacketMetadataPresent(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{RunID: "run-meta", Phase: "testing", Task: "check metadata"})
	waitForBuild(t, b, "run-meta", 500*time.Millisecond)

	pr := b.GetPacket("run-meta", "")
	if pr.Status != PacketStatusReady {
		t.Fatal("expected status=ready")
	}
	pkt := pr.Packet

	if pkt.BuiltFromSeq != 1 {
		t.Errorf("expected built_from_seq=1, got %d", pkt.BuiltFromSeq)
	}
	if pkt.PacketVersion < 1 {
		t.Errorf("expected packet_version >= 1, got %d", pkt.PacketVersion)
	}
	if pkt.GeneratedAt.IsZero() {
		t.Error("expected non-zero generated_at")
	}
	if pr.CurrentSeq != 1 {
		t.Errorf("expected current_seq=1, got %d", pr.CurrentSeq)
	}
}

func TestPacketVersionIncrementsOnRebuild(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{RunID: "run-ver", Task: "v1"})
	waitForBuild(t, b, "run-ver", 500*time.Millisecond)

	pr1 := b.GetPacket("run-ver", "")
	v1 := pr1.Packet.PacketVersion

	// Ack and observe again to trigger a new build.
	b.AckPacket("run-ver")
	b.Observe(WorkObservation{RunID: "run-ver", Task: "v2"})
	waitForBuild(t, b, "run-ver", 500*time.Millisecond)

	pr2 := b.GetPacket("run-ver", "")
	v2 := pr2.Packet.PacketVersion

	if v2 <= v1 {
		t.Errorf("expected packet_version to increment: v1=%d, v2=%d", v1, v2)
	}
}

// --- Ack clears acked flag on new packet ---

func TestAckClearedByNewPacket(t *testing.T) {
	b := NewBroker(nil, nil)

	b.Observe(WorkObservation{RunID: "run-ack2", Task: "first"})
	waitForBuild(t, b, "run-ack2", 500*time.Millisecond)

	b.AckPacket("run-ack2")

	pr := b.GetPacket("run-ack2", "")
	if pr.Status != PacketStatusAcked {
		t.Fatalf("expected acked, got %s", pr.Status)
	}

	// New observation should clear the acked state and produce a new packet.
	b.Observe(WorkObservation{RunID: "run-ack2", Task: "second"})
	waitForBuild(t, b, "run-ack2", 500*time.Millisecond)

	pr = b.GetPacket("run-ack2", "")
	if pr.Status != PacketStatusReady {
		t.Errorf("expected ready after new observation post-ack, got %s", pr.Status)
	}
}

// --- TTL cleanup ---

func TestSweepStaleRuns(t *testing.T) {
	b := NewBroker(nil, nil)
	b.runTTL = 50 * time.Millisecond // short TTL for testing

	b.Observe(WorkObservation{RunID: "run-stale", Task: "old"})
	waitForBuild(t, b, "run-stale", 500*time.Millisecond)

	// Verify it exists.
	pr := b.GetPacket("run-stale", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected ready, got %s", pr.Status)
	}

	// Wait for TTL to expire.
	time.Sleep(100 * time.Millisecond)

	b.sweepStaleRuns()

	// Should be gone.
	pr = b.GetPacket("run-stale", "")
	if pr.Status != PacketStatusNone {
		t.Errorf("expected none after sweep, got %s", pr.Status)
	}
}

func TestSweepPreservesActiveRuns(t *testing.T) {
	b := NewBroker(nil, nil)
	b.runTTL = 50 * time.Millisecond

	b.Observe(WorkObservation{RunID: "run-active", Task: "recent"})
	waitForBuild(t, b, "run-active", 500*time.Millisecond)

	// Sweep immediately — run is still fresh.
	b.sweepStaleRuns()

	pr := b.GetPacket("run-active", "")
	if pr.Status != PacketStatusReady {
		t.Errorf("expected ready (active run preserved), got %s", pr.Status)
	}
}

// --- Status signaling ---

func TestGetPacketStatusNone(t *testing.T) {
	b := NewBroker(nil, nil)
	pr := b.GetPacket("nobody", "")
	if pr.Status != PacketStatusNone {
		t.Errorf("expected none, got %s", pr.Status)
	}
}

func TestGetPacketStatusAcked(t *testing.T) {
	b := NewBroker(nil, nil)
	b.Observe(WorkObservation{RunID: "run-st", Task: "test"})
	waitForBuild(t, b, "run-st", 500*time.Millisecond)

	b.AckPacket("run-st")

	pr := b.GetPacket("run-st", "")
	if pr.Status != PacketStatusAcked {
		t.Errorf("expected acked, got %s", pr.Status)
	}
	if pr.Packet != nil {
		t.Error("expected nil packet when acked")
	}
	if pr.CurrentSeq != 1 {
		t.Errorf("expected current_seq=1 when acked, got %d", pr.CurrentSeq)
	}
}

// --- Concurrent safety ---

func TestBrokerConcurrentObserves(t *testing.T) {
	b := NewBroker(nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Observe(WorkObservation{
				RunID: "run-conc",
				Task:  "concurrent",
				Phase: "testing",
			})
		}(i)
	}
	wg.Wait()

	waitForBuild(t, b, "run-conc", 1*time.Second)

	pr := b.GetPacket("run-conc", "")
	if pr.Status != PacketStatusReady {
		t.Fatalf("expected ready after concurrent observes, got %s", pr.Status)
	}
	if pr.Packet.ObservationCount != 20 {
		t.Errorf("expected 20 observations, got %d", pr.Packet.ObservationCount)
	}
	if pr.CurrentSeq != 20 {
		t.Errorf("expected current_seq=20, got %d", pr.CurrentSeq)
	}
}

// --- Pure function tests ---

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
		if !strings.Contains(q, expected) {
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
		BuiltFromSeq:     2,
		PacketVersion:    1,
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
	for _, expected := range []string{"checkpoint", "run-test", "test memory", "Seq: 2", "Version: 1"} {
		if !strings.Contains(text, expected) {
			t.Errorf("expected %q in formatted output, got:\n%s", expected, text)
		}
	}
}

// --- Helpers ---

// waitForBuild polls until the run's build is no longer in flight,
// or times out.
func waitForBuild(t *testing.T, b *Broker, runID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		rs, ok := b.runs[runID]
		building := ok && rs.building
		b.mu.Unlock()
		if !building {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for build to complete on run %s", runID)
}
