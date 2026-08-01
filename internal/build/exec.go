package build

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// BuildResult is the outcome of building a game's source directory.
type BuildResult struct {
	ProjectType ProjectType
	PdxPath     string
	Output      string
}

// Build detects sourceDir's project type and builds it into a .pdx.
//
// paths is the SDK the caller resolved. It is a parameter because this package
// used to read PLAYDATE_SDK_PATH out of the environment itself, independently of
// the server that had already resolved an SDK. That was a latent split brain:
// any resolution more capable than a bare environment variable - a config file,
// a default install location - would have been honoured by launch_simulator and
// silently ignored by build_game, and the two would have used different SDKs
// without either of them noticing.
func Build(sourceDir string, paths sdk.Paths) (BuildResult, error) {
	projectType, err := DetectProjectType(sourceDir)
	if err != nil {
		return BuildResult{}, err
	}
	switch projectType {
	case ProjectTypeC:
		return buildC(sourceDir, paths)
	case ProjectTypeLua:
		return buildLua(sourceDir, paths)
	default:
		return BuildResult{}, fmt.Errorf("unknown project type %v", projectType)
	}
}

// buildC runs the SDK's CMake support, which invokes pdc itself as a
// POST_BUILD step and names the resulting .pdx after the game's own
// CMakeLists.txt (PLAYDATE_GAME_NAME), so the path has to be discovered
// with locatePDX rather than predicted.
func buildC(sourceDir string, paths sdk.Paths) (BuildResult, error) {
	// Checked before running anything, so a missing toolchain says so instead of
	// surfacing as whatever cmake's absence looks like to exec. The container has
	// cmake baked in; a host often does not.
	//
	// Deliberately not checking arm-none-eabi-gcc: the Simulator build produces a
	// shared library with the host compiler, and the ARM toolchain only matters
	// for device builds, which this server never does.
	if _, err := exec.LookPath("cmake"); err != nil {
		return BuildResult{}, fmt.Errorf(
			"cmake is not on PATH, and a C game needs it to build. Install it and restart " +
				"your MCP client, so the server inherits the updated PATH")
	}

	var output bytes.Buffer
	for _, args := range [][]string{
		{"-S", ".", "-B", "build"},
		{"--build", "build"},
	} {
		cmd := exec.Command("cmake", args...)
		cmd.Dir = sourceDir
		// The game's own CMakeLists.txt reads $ENV{PLAYDATE_SDK_PATH}. Without
		// this it would be unset whenever the SDK came from anywhere but the
		// environment, and the SDK's CMake template falls back to shelling out to
		// bash, egrep, head and cut against ~/.Playdate/config - none of which
		// exist on Windows, and which would silently pick a different SDK than the
		// one this server resolved.
		cmd.Env = paths.BuildEnv()
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
func buildLua(sourceDir string, paths sdk.Paths) (BuildResult, error) {
	pdxPath := filepath.Join(sourceDir, filepath.Base(sourceDir)+".pdx")

	var output bytes.Buffer
	// paths.PDC rather than a joined "bin/pdc": that is where the .exe suffix on
	// Windows comes from. -sdkpath was already correct and portable.
	cmd := exec.Command(paths.PDC, "-sdkpath", paths.Root, filepath.Join(sourceDir, "Source"), pdxPath)
	cmd.Env = paths.BuildEnv()
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return BuildResult{Output: output.String()}, fmt.Errorf("pdc: %w", err)
	}

	return BuildResult{ProjectType: ProjectTypeLua, PdxPath: pdxPath, Output: output.String()}, nil
}
