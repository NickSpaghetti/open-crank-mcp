package build

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NickSpaghetti/open-crank-mcp/internal/sdk"
)

// isStaleCMakeCache reports whether cmake's output is the "this cache was
// generated somewhere else" complaint. Both halves must match before we delete anything.
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

// Build detects sourceDir's project type and builds it into a .pdx using the
// caller's resolved SDK, so building and launching agree on one SDK.
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

// cmakeCommand prefers CMake on PATH. Visual Studio Build Tools also ships
// CMake without adding it to PATH, so Windows looks for that copy through
// vswhere before reporting the missing toolchain.
func cmakeCommand() (string, error) {
	if path, err := exec.LookPath("cmake"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		if programFilesX86 != "" {
			vswhere := filepath.Join(programFilesX86, "Microsoft Visual Studio", "Installer", "vswhere.exe")
			output, err := exec.Command(vswhere, "-all", "-products", "*", "-property", "installationPath").Output()
			if err == nil {
				for _, installation := range strings.Split(strings.TrimSpace(string(output)), "\n") {
					candidate := filepath.Join(strings.TrimSpace(installation), "Common7", "IDE", "CommonExtensions", "Microsoft", "CMake", "CMake", "bin", "cmake.exe")
					if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
						return candidate, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("cmake is not on PATH; install CMake or Visual Studio's CMake component and restart your MCP client")
}

// buildC runs the SDK's CMake support, which invokes pdc as a POST_BUILD step.
func buildC(sourceDir string, paths sdk.Paths) (BuildResult, error) {
	// No arm-none-eabi-gcc check: Simulator builds use the host compiler.
	cmake, err := cmakeCommand()
	if err != nil {
		return BuildResult{}, err
	}

	var output bytes.Buffer
	runCMake := func(args []string) error {
		cmd := exec.Command(cmake, args...)
		cmd.Dir = sourceDir
		cmd.Env = paths.BuildEnv()
		cmd.Stdout = &output
		cmd.Stderr = &output
		return cmd.Run()
	}

	configure := []string{"-S", ".", "-B", "build"}
	if err := runCMake(configure); err != nil {
		// cmake refuses a cache generated at another absolute path, which happens when
		// one game is built in the container (/workspace) and natively. Clear it and retry.
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

// buildLua runs pdc directly against sourceDir/Source.
func buildLua(sourceDir string, paths sdk.Paths) (BuildResult, error) {
	pdxPath := filepath.Join(sourceDir, filepath.Base(sourceDir)+".pdx")

	var output bytes.Buffer
	cmd := exec.Command(paths.PDC, "-sdkpath", paths.Root, filepath.Join(sourceDir, "Source"), pdxPath)
	cmd.Env = paths.BuildEnv()
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return BuildResult{Output: output.String()}, fmt.Errorf("pdc: %w", err)
	}

	return BuildResult{ProjectType: ProjectTypeLua, PdxPath: pdxPath, Output: output.String()}, nil
}
