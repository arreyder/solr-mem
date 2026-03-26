package main

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolOutput wraps text and optional structured content for a tool response.
type ToolOutput struct {
	Text       string
	Structured any
}

func TextResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func ErrorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}
