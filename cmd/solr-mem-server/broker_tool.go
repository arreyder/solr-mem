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

		pending := broker.Observe(obs)

		return ToolOutput{
			Text: fmt.Sprintf("Observation recorded for run %s (%d pending). Packet building in background.", runID, pending),
			Structured: map[string]any{
				"run_id":  runID,
				"pending": pending,
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

		pkt := broker.GetPacket(runID, phase)
		if pkt == nil {
			return ToolOutput{
				Text: fmt.Sprintf("No packet ready for run %s", runID),
				Structured: map[string]any{
					"run_id": runID,
					"status": "none",
				},
			}, nil
		}

		return ToolOutput{
			Text:       formatPacket(pkt),
			Structured: pkt,
		}, nil
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

func formatPacket(pkt *MemoryPacket) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Memory Packet [%s] ===\n", pkt.Delivery))
	sb.WriteString(fmt.Sprintf("Run: %s | Phase: %s | Observations: %d\n", pkt.RunID, pkt.Phase, pkt.ObservationCount))
	sb.WriteString(fmt.Sprintf("Generated: %s\n\n", pkt.GeneratedAt.Format("15:04:05")))

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
