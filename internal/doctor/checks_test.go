package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakePDC writes an executable that prints a known string, so the assertion is
// about pass-through rather than about whatever a real tool happens to say.
//
// The first attempt at this used /bin/echo and asserted its output contained
// "--version"; GNU echo treats that as its own flag and prints a version banner,
// so the test failed for a reason that had nothing to do with the code.
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

// cmd/smoke-check prints PDCVersion's return value verbatim, so swallowing the
// output would silently empty its report. That is what this pins.
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

// dynamicBinary finds something on this machine that really is dynamically
// linked, or skips.
//
// A Go test binary is not a candidate: it is usually static, and ldd answers
// "not a dynamic executable" with exit 1 - which is how the first version of
// this test failed. The Simulator this check exists for is dynamic, so a dynamic
// stand-in is the honest analogue.
func dynamicBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		// Off Linux SharedLibraries only stats the path, so the test executable
		// itself is a portable stand-in.
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

// The "everything resolves" direction. The opposite one - a genuinely missing
// library - needs a deliberately broken binary to reproduce and is left to the
// real Simulator, which is what cmd/smoke-check launches.
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
