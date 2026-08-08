package tools

import (
	"context"
	"fmt"
	"strings"

	crankSetup "github.com/NickSpaghetti/open-crank-mcp/internal/setup"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SetupInput struct {
	SourceDir string `json:"source_dir" jsonschema:"path to the game's source directory"`
	Language  string `json:"language,omitempty" jsonschema:"one of lua, c, hybrid; omit to auto-detect"`
}

type SetupOutput struct {
	Language     string                  `json:"language"`
	FilesCopied  []string                `json:"files_copied,omitempty"`
	FilesPatched []crankSetup.FileChange `json:"files_patched,omitempty"`
	ManualSteps  []string                `json:"manual_steps,omitempty"`
}

func resolveLanguage(sourceDir, override string) (crankSetup.Language, error) {
	if override != "" {
		if !crankSetup.ValidLanguage(crankSetup.Language(override)) {
			return "", fmt.Errorf("unknown language %q, want one of %s",
				override, strings.Join(crankSetup.LanguageNames(), ", "))
		}
		return crankSetup.Language(override), nil
	}
	return crankSetup.DetectLanguage(sourceDir)
}

func (s *Server) setupHarness(_ context.Context, _ *mcp.CallToolRequest, in SetupInput) (*mcp.CallToolResult, SetupOutput, error) {
	language, err := resolveLanguage(in.SourceDir, in.Language)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, SetupOutput{}, nil
	}

	result, err := crankSetup.Setup(in.SourceDir, language, s.harnessFS)
	out := SetupOutput{
		Language:     string(result.Language),
		FilesCopied:  result.FilesCopied,
		FilesPatched: result.FilesPatched,
		ManualSteps:  result.ManualSteps,
	}
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("setup failed: %v", err)}},
		}, out, nil
	}
	return nil, out, nil
}

type TeardownInput struct {
	SourceDir string `json:"source_dir" jsonschema:"path to the game's source directory"`
	Language  string `json:"language,omitempty" jsonschema:"one of lua, c, hybrid; omit to auto-detect"`
}

type TeardownOutput struct {
	FilesRemoved []string                `json:"files_removed,omitempty"`
	FilesPatched []crankSetup.FileChange `json:"files_patched,omitempty"`
}

func (s *Server) teardownHarness(_ context.Context, _ *mcp.CallToolRequest, in TeardownInput) (*mcp.CallToolResult, TeardownOutput, error) {
	language, err := resolveLanguage(in.SourceDir, in.Language)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, TeardownOutput{}, nil
	}

	result, err := crankSetup.Teardown(in.SourceDir, language)
	out := TeardownOutput{
		FilesRemoved: result.FilesRemoved,
		FilesPatched: result.FilesPatched,
	}
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("teardown failed: %v", err)}},
		}, out, nil
	}
	return nil, out, nil
}
