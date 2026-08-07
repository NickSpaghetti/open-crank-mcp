package build

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBundleIDFindsIt(t *testing.T) {
	dir := t.TempDir()
	content := "name=Test Game\nauthor=someone\nbundleID=com.example.testgame\n"
	if err := os.WriteFile(filepath.Join(dir, "pdxinfo"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := ReadBundleID(dir)
	if err != nil {
		t.Fatalf("ReadBundleID: %v", err)
	}
	if got != "com.example.testgame" {
		t.Fatalf("ReadBundleID() = %q, want %q", got, "com.example.testgame")
	}
}

func TestReadBundleIDMissingFileIsAnError(t *testing.T) {
	dir := t.TempDir()

	if _, err := ReadBundleID(dir); err == nil {
		t.Fatal("ReadBundleID: expected an error for a missing pdxinfo, got nil")
	}
}

func TestReadBundleIDMissingKeyIsAnError(t *testing.T) {
	dir := t.TempDir()
	content := "name=Test Game\nauthor=someone\n"
	if err := os.WriteFile(filepath.Join(dir, "pdxinfo"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := ReadBundleID(dir); err == nil {
		t.Fatal("ReadBundleID: expected an error when bundleID is absent, got nil")
	}
}

func TestReadBundleIDScanErrorIsAnError(t *testing.T) {
	dir := t.TempDir()
	// A line longer than bufio.Scanner's default max token size makes
	// Scan() stop with bufio.ErrTooLong - a real scan error, distinct from
	// simply reaching EOF without finding bundleID.
	longLine := strings.Repeat("x", bufio.MaxScanTokenSize+1)
	if err := os.WriteFile(filepath.Join(dir, "pdxinfo"), []byte(longLine), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadBundleID(dir)
	if err == nil {
		t.Fatal("ReadBundleID: expected a scan error, got nil")
	}
	if !strings.Contains(err.Error(), "reading") {
		t.Fatalf("ReadBundleID error = %q, want it to report a read/scan failure, not just \"no bundleID found\"", err.Error())
	}
}

// pdc builds a project whose pdxinfo has no bundleID, and synthesises one
// carrying only pdxversion and buildtime. Several of the SDK's own Lua examples
// ship with no pdxinfo at all, so this is an ordinary thing for a user to hit
// rather than a corrupt build - and it fails at launch_simulator, two successful
// tool calls after the actual cause.
//
// So the error has to name the remedy, not just the absence.
func TestReadBundleIDMissingBundleIDExplainsItself(t *testing.T) {
	dir := t.TempDir()
	pdx := filepath.Join(dir, "game.pdx")
	if err := os.MkdirAll(pdx, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Exactly what pdc writes for a project with no bundleID.
	if err := os.WriteFile(filepath.Join(pdx, "pdxinfo"),
		[]byte("pdxversion=30101\nbuildtime=839356623\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadBundleID(pdx)
	if err == nil {
		t.Fatal("ReadBundleID accepted a pdxinfo with no bundleID")
	}
	for _, want := range []string{"bundleID=", "pdxinfo", "data directory"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q, so it says what is missing but not what to do:\n%v", want, err)
		}
	}
}
