package tools

import (
	"context"
	"fmt"

	"github.com/NickSpaghetti/open-crank-mcp/internal/build"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BuildGameInput struct {
	SourceDir string `json:"source_dir" jsonschema:"path to the game's source directory"`
}

type BuildGameOutput struct {
	ProjectType string `json:"project_type"`
	PdxPath     string `json:"pdx_path"`
	Output      string `json:"output"`
}

func (s *Server) buildGame(_ context.Context, _ *mcp.CallToolRequest, in BuildGameInput) (*mcp.CallToolResult, BuildGameOutput, error) {
	result, err := build.Build(in.SourceDir)
	out := BuildGameOutput{
		ProjectType: result.ProjectType.String(),
		PdxPath:     result.PdxPath,
		Output:      result.Output,
	}
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("build failed: %v\n%s", err, result.Output)}},
		}, out, nil
	}
	return nil, out, nil
}
