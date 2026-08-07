package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// isStaleCMakeCache reports whether cmake's output is the "this cache was
// generated somewhere else" complaint, as opposed to any other configure
// failure. Matched on cmake's own wording, which has been stable for years, and
// on both halves it prints so a single stray mention cannot trigger a delete.
func isStaleCMakeCache(out string) bool {
	return strings.Contains(out, "CMakeCache.txt directory") &&
		strings.Contains(out, "is different than the directory")
}

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
	runCMake := func(args []string) error {
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
		return cmd.Run()
	}

	configure := []string{"-S", ".", "-B", "build"}
	if err := runCMake(configure); err != nil {
		// A CMakeCache.txt records the absolute paths it was generated with, and
		// cmake refuses to reuse one generated somewhere else. That is not an
		// exotic case here: this project deliberately supports building the same
		// game directory two ways, and the container sees it at /workspace while a
		// native run sees it at its real path. Build in one mode, then the other,
		// and the second fails on a cache the user never knew existed.
		//
		// So: on that specific failure, discard the stale cache and configure
		// again. Scoped to the mismatch message rather than any cmake error, since
		// deleting a build directory is not something to do on a compile error, and
		// reported in the output rather than silently.
		if !isStaleCMakeCache(output.String()) {
			return BuildResult{Output: output.String()}, fmt.Errorf("cmake %v: %w", configure, err)
		}
		buildDir := filepath.Join(sourceDir, "build")
		fmt.Fprintf(&output, "\n[open-crank-mcp] cmake cache in %s was generated for a different "+
			"path, most likely by building this game in the other mode. Removing it and "+
			"reconfiguring.\n", buildDir)
		if rmErr := os.RemoveAll(buildDir); rmErr != nil {
			return BuildResult{Output: output.String()}, fmt.Errorf("removing stale cmake cache: %w", rmErr)
		}
		if err := runCMake(configure); err != nil {
			return BuildResult{Output: output.String()}, fmt.Errorf("cmake %v after clearing a stale cache: %w", configure, err)
		}
	}

	if err := runCMake([]string{"--build", "build"}); err != nil {
		return BuildResult{Output: output.String()}, fmt.Errorf("cmake --build: %w", err)
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
