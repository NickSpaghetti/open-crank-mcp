package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakePDC writes an executable that prints a known string. Not /bin/echo: GNU
// echo treats --version as its own flag.
func fakePDC(t *testing.T, prints string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this fake pdc depends on a Unix shebang script")
	}
	path := filepath.Join(t.TempDir(), "pdc")
	script := "#!/bin/sh\necho '" + prints + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the stand-in: %v", err)
	}
	return path
}

func TestPDCVersionReturnsOutput(t *testing.T) {
	out, err := PDCVersion(fakePDC(t, "3.1.1"))
	if err != nil {
		t.Fatalf("PDCVersion against a stand-in: %v", err)
	}
	if strings.TrimSpace(out) != "3.1.1" {
		t.Errorf("PDCVersion returned %q, want the tool's own output passed through", out)
	}
}

func TestPDCVersionReportsAMissingBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "pdc")
	out, err := PDCVersion(missing)
	if err == nil {
		t.Fatalf("PDCVersion against a nonexistent path succeeded, returning %q", out)
	}
	if !strings.Contains(err.Error(), "pdc --version") {
		t.Errorf("PDCVersion error is %q, want it to name the command that failed", err)
	}
}

// dynamicBinary finds a dynamically linked binary on this machine, or skips. Not
// the Go test binary: it is usually static, and ldd exits 1 on it.
func dynamicBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		path, err := os.Executable()
		if err != nil {
			t.Fatalf("locating the test executable: %v", err)
		}
		return path
	}
	for _, candidate := range []string{"/bin/sh", "/bin/ls", "/usr/bin/env"} {
		out, err := exec.Command("ldd", candidate).CombinedOutput()
		if err == nil && !strings.Contains(string(out), "not a dynamic executable") {
			return candidate
		}
	}
	t.Skip("no dynamically linked binary found to check against")
	return ""
}

// The missing-library direction is left to cmd/smoke-check's real Simulator.
func TestSharedLibrariesAcceptsAResolvableBinary(t *testing.T) {
	if err := SharedLibraries(dynamicBinary(t)); err != nil {
		t.Errorf("SharedLibraries rejected a binary that does resolve: %v", err)
	}
}

func TestSharedLibrariesRejectsAMissingBinary(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "PlaydateSimulator")
	if err := SharedLibraries(missing); err == nil {
		t.Error("SharedLibraries accepted a path with no binary at it")
	}
}
