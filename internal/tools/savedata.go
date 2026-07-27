package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReadSaveDataInput struct {
	Filename string `json:"filename,omitempty" jsonschema:"specific file to read; omit to list available files instead"`
}

type ReadSaveDataOutput struct {
	Files []string `json:"files,omitempty"`
	Data  any      `json:"data,omitempty"`
}

func (s *Server) readSaveData(_ context.Context, _ *mcp.CallToolRequest, in ReadSaveDataInput) (*mcp.CallToolResult, ReadSaveDataOutput, error) {
	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, ReadSaveDataOutput{}, wrapErr
	}

	if in.Filename == "" {
		entries, err := os.ReadDir(dataDir)
		if err != nil {
			return nil, ReadSaveDataOutput{}, fmt.Errorf("listing %s: %w", dataDir, err)
		}
		var files []string
		for _, e := range entries {
			// "mcp" is the harness IPC directory, not save data - excluded
			// so this listing stays focused on what a caller actually asked
			// about.
			if e.Name() == "mcp" {
				continue
			}
			files = append(files, e.Name())
		}
		return nil, ReadSaveDataOutput{Files: files}, nil
	}

	b, err := os.ReadFile(filepath.Join(dataDir, in.Filename))
	if err != nil {
		return nil, ReadSaveDataOutput{}, fmt.Errorf("reading %s: %w", in.Filename, err)
	}
	var data any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, ReadSaveDataOutput{}, fmt.Errorf("parsing %s as JSON: %w", in.Filename, err)
	}
	return nil, ReadSaveDataOutput{Data: data}, nil
}

type WriteSaveDataInput struct {
	Filename string `json:"filename" jsonschema:"file to write, relative to the game's data directory"`
	Data     any    `json:"data" jsonschema:"JSON value to write"`
}

type WriteSaveDataOutput struct{}

func (s *Server) writeSaveData(_ context.Context, _ *mcp.CallToolRequest, in WriteSaveDataInput) (*mcp.CallToolResult, WriteSaveDataOutput, error) {
	dataDir, err := s.requireDataDir()
	if err != nil {
		result, wrapErr := handleRoundTripErr(err)
		return result, WriteSaveDataOutput{}, wrapErr
	}

	b, err := json.Marshal(in.Data)
	if err != nil {
		return nil, WriteSaveDataOutput{}, fmt.Errorf("marshaling data: %w", err)
	}
	path := filepath.Join(dataDir, in.Filename)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return nil, WriteSaveDataOutput{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return nil, WriteSaveDataOutput{}, nil
}
