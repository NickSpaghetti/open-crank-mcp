package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCMakeCommandFindsVisualStudioCopyWithoutPATH(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Visual Studio's bundled CMake is a Windows fallback")
	}

	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	if programFilesX86 == "" {
		t.Skip("ProgramFiles(x86) is unavailable")
	}
	vswhere := filepath.Join(programFilesX86, "Microsoft Visual Studio", "Installer", "vswhere.exe")
	if _, err := os.Stat(vswhere); err != nil {
		t.Skip("Visual Studio is not installed")
	}
	instances, err := exec.Command(vswhere, "-all", "-products", "*", "-property", "installationPath").Output()
	if err != nil {
		t.Fatalf("vswhere: %v", err)
	}
	var bundledCMake string
	for _, instance := range strings.Split(strings.TrimSpace(string(instances)), "\n") {
		candidate := filepath.Join(strings.TrimSpace(instance), "Common7", "IDE", "CommonExtensions", "Microsoft", "CMake", "CMake", "bin", "cmake.exe")
		if _, err := os.Stat(candidate); err == nil {
			bundledCMake = candidate
			break
		}
	}
	if bundledCMake == "" {
		t.Skip("no Visual Studio installation with bundled CMake was found")
	}

	oldPath := os.Getenv("PATH")
	var withoutCMake []string
	for _, dir := range filepath.SplitList(oldPath) {
		if _, err := os.Stat(filepath.Join(dir, "cmake.exe")); err == nil {
			continue
		}
		withoutCMake = append(withoutCMake, dir)
	}
	t.Setenv("PATH", strings.Join(withoutCMake, string(os.PathListSeparator)))

	got, err := cmakeCommand()
	if err != nil {
		t.Fatalf("cmakeCommand without PATH CMake: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(bundledCMake)) {
		t.Fatalf("cmakeCommand() = %q, want Visual Studio CMake %q", got, bundledCMake)
	}
}

// Clearing a build directory is destructive, so this must not fire on an
// ordinary configure or compile error.
func TestIsStaleCMakeCache(t *testing.T) {
	stale := `CMake Error: The current CMakeCache.txt directory /workspace/game/build/CMakeCache.txt ` +
		`is different than the directory /home/$USER/game/build where CMakeCache.txt was created.`
	if !isStaleCMakeCache(stale) {
		t.Error("cmake's own cache-mismatch wording was not recognised")
	}

	for name, out := range map[string]string{
		"a compile error":        "error: 'foo' undeclared (first use in this function)",
		"a missing SDK":          "CMake Error: SDK Path not found; set ENV value PLAYDATE_SDK_PATH",
		"empty output":           "",
		"only half the phrasing": "CMake Error: The current CMakeCache.txt directory is fine",
	} {
		if isStaleCMakeCache(out) {
			t.Errorf("%s was mistaken for a stale cache, which would delete the build directory:\n%s", name, out)
		}
	}
}
