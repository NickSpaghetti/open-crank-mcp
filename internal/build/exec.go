package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildResult is the outcome of building a game's source directory.
type BuildResult struct {
	ProjectType ProjectType
	PdxPath     string
	Output      string
}

// Build detects sourceDir's project type and builds it into a .pdx.
func Build(sourceDir string) (BuildResult, error) {
	projectType, err := DetectProjectType(sourceDir)
	if err != nil {
		return BuildResult{}, err
	}
	switch projectType {
	case ProjectTypeC:
		return buildC(sourceDir)
	case ProjectTypeLua:
		return buildLua(sourceDir)
	default:
		return BuildResult{}, fmt.Errorf("unknown project type %v", projectType)
	}
}

// buildC runs the SDK's CMake support, which invokes pdc itself as a
// POST_BUILD step and names the resulting .pdx after the game's own
// CMakeLists.txt (PLAYDATE_GAME_NAME), so the path has to be discovered
// with locatePDX rather than predicted.
func buildC(sourceDir string) (BuildResult, error) {
	var output bytes.Buffer
	for _, args := range [][]string{
		{"-S", ".", "-B", "build"},
		{"--build", "build"},
	} {
		cmd := exec.Command("cmake", args...)
		cmd.Dir = sourceDir
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil {
			return BuildResult{Output: output.String()}, fmt.Errorf("cmake %v: %w", args, err)
		}
	}

	pdxPath, err := locatePDX(sourceDir)
	if err != nil {
		return BuildResult{Output: output.String()}, err
	}
	return BuildResult{ProjectType: ProjectTypeC, PdxPath: pdxPath, Output: output.String()}, nil
}

// buildLua runs pdc directly against sourceDir/Source. There's no CMake
// involved and no ambiguity about the output path: we choose it ourselves.
func buildLua(sourceDir string) (BuildResult, error) {
	sdkPath := os.Getenv("PLAYDATE_SDK_PATH")
	if sdkPath == "" {
		return BuildResult{}, fmt.Errorf("PLAYDATE_SDK_PATH is not set")
	}
	pdcBin := filepath.Join(sdkPath, "bin", "pdc")
	pdxPath := filepath.Join(sourceDir, filepath.Base(sourceDir)+".pdx")

	var output bytes.Buffer
	cmd := exec.Command(pdcBin, "-sdkpath", sdkPath, filepath.Join(sourceDir, "Source"), pdxPath)
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return BuildResult{Output: output.String()}, fmt.Errorf("pdc: %w", err)
	}

	return BuildResult{ProjectType: ProjectTypeLua, PdxPath: pdxPath, Output: output.String()}, nil
}
