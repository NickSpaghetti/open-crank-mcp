package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetLogsInput struct {
	TailN int `json:"tail_n,omitempty" jsonschema:"number of trailing lines to return; 0 or omitted returns everything"`
}

type GetLogsOutput struct {
	Lines []string `json:"lines"`
}

func (s *Server) getLogs(_ context.Context, _ *mcp.CallToolRequest, in GetLogsInput) (*mcp.CallToolResult, GetLogsOutput, error) {
	s.mu.Lock()
	sim := s.sim
	s.mu.Unlock()
	if sim == nil {
		return notRunningResult(), GetLogsOutput{}, nil
	}

	output := sim.Output()
	var lines []string
	if output != "" {
		lines = strings.Split(strings.TrimRight(output, "\n"), "\n")
	}
	if in.TailN > 0 && in.TailN < len(lines) {
		lines = lines[len(lines)-in.TailN:]
	}
	return nil, GetLogsOutput{Lines: lines}, nil
}
