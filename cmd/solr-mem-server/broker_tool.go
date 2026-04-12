package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func observeWorkTool(broker *Broker) ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		runID := getString(args, "run_id")
		if runID == "" {
			return nil, fmt.Errorf("run_id is required")
		}

		obs := WorkObservation{
			RunID:       runID,
			AgentID:     getString(args, "agent_id"),
			Repo:        getString(args, "repo"),
			Phase:       getString(args, "phase"),
			Task:        getString(args, "task"),
			Subgoal:     getString(args, "subgoal"),
			Entities:    getStringSlice(args, "entities"),
			CodeRefs:    getStringSlice(args, "code_refs"),
			Uncertainty: getString(args, "uncertainty"),
			NextAction:  getString(args, "next_action"),
		}

		result := broker.Observe(obs)

		return ToolOutput{
			Text: fmt.Sprintf("Observation recorded for run %s (seq %d, %d pending). Packet building in background.",
				runID, result.Seq, result.Pending),
			Structured: map[string]any{
				"run_id":  runID,
				"seq":     result.Seq,
				"pending": result.Pending,
				"status":  "accepted",
			},
		}, nil
	}
}

func getMemoryPacketTool(broker *Broker) ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		runID := getString(args, "run_id")
		if runID == "" {
			return nil, fmt.Errorf("run_id is required")
		}
		phase := getString(args, "phase")

		pr := broker.GetPacket(runID, phase)

		switch pr.Status {
		case PacketStatusReady:
			return ToolOutput{
				Text:       formatPacket(pr.Packet),
				Structured: formatPacketResult(runID, pr),
			}, nil

		case PacketStatusBuilding:
			return ToolOutput{
				Text: fmt.Sprintf("Packet for run %s is still building (seq %d). Try again shortly.", runID, pr.CurrentSeq),
				Structured: formatPacketResult(runID, pr),
			}, nil

		case PacketStatusAcked:
			return ToolOutput{
				Text: fmt.Sprintf("Packet for run %s was already acknowledged (seq %d). Send a new observation to generate a fresh packet.", runID, pr.CurrentSeq),
				Structured: formatPacketResult(runID, pr),
			}, nil

		default: // PacketStatusNone
			return ToolOutput{
				Text: fmt.Sprintf("No packet for run %s. Call observe_work first.", runID),
				Structured: formatPacketResult(runID, pr),
			}, nil
		}
	}
}

func ackMemoryPacketTool(broker *Broker) ToolHandler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		runID := getString(args, "run_id")
		if runID == "" {
			return nil, fmt.Errorf("run_id is required")
		}

		acked := broker.AckPacket(runID)
		status := "acked"
		msg := fmt.Sprintf("Packet acknowledged for run %s", runID)
		if !acked {
			status = "none"
			msg = fmt.Sprintf("No packet to acknowledge for run %s", runID)
		}

		return ToolOutput{
			Text: msg,
			Structured: map[string]any{
				"run_id": runID,
				"status": status,
			},
		}, nil
	}
}

// formatPacketResult builds the structured response for get_memory_packet.
func formatPacketResult(runID string, pr PacketResult) map[string]any {
	result := map[string]any{
		"run_id":      runID,
		"status":      string(pr.Status),
		"current_seq": pr.CurrentSeq,
	}
	if pr.Packet != nil {
		result["packet"] = pr.Packet
	}
	return result
}

func formatPacket(pkt *MemoryPacket) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Memory Packet [%s] ===\n", pkt.Delivery))
	sb.WriteString(fmt.Sprintf("Run: %s | Phase: %s | Observations: %d\n", pkt.RunID, pkt.Phase, pkt.ObservationCount))
	sb.WriteString(fmt.Sprintf("Generated: %s | Seq: %d | Version: %d\n\n",
		pkt.GeneratedAt.Format("15:04:05"), pkt.BuiltFromSeq, pkt.PacketVersion))

	if pkt.Summary != "" {
		sb.WriteString(pkt.Summary)
		sb.WriteString("\n\n")
	}

	for i, item := range pkt.Items {
		sb.WriteString(fmt.Sprintf("--- Item %d [%s] (relevance: %.2f) ---\n", i+1, item.Source, item.Relevance))
		if item.Title != "" {
			sb.WriteString(fmt.Sprintf("Title: %s\n", item.Title))
		}
		if item.FilePath != "" {
			sb.WriteString(fmt.Sprintf("File: %s\n", item.FilePath))
		}
		if item.SymbolName != "" {
			sb.WriteString(fmt.Sprintf("Symbol: %s\n", item.SymbolName))
		}
		if item.MemoryType != "" {
			sb.WriteString(fmt.Sprintf("Type: %s\n", item.MemoryType))
		}
		if item.Summary != "" {
			sb.WriteString(fmt.Sprintf("Summary: %s\n", item.Summary))
		}
		sb.WriteString(fmt.Sprintf("Reason: %s\n", item.Reason))
		sb.WriteString(fmt.Sprintf("ID: %s\n", item.SourceID))
		if len(item.Tags) > 0 {
			tagsJSON, _ := json.Marshal(item.Tags)
			sb.WriteString(fmt.Sprintf("Tags: %s\n", tagsJSON))
		}
		sb.WriteString("\n")
	}

	if len(pkt.Items) == 0 {
		sb.WriteString("(no relevant items found)\n")
	}

	return sb.String()
}
