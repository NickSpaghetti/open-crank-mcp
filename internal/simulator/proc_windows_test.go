//go:build windows

package simulator

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestProcessStartsOnlyAfterJobAssignment(t *testing.T) {
	cmdPath, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skipf("cmd.exe is not available: %v", err)
	}
	cmd := exec.Command(cmdPath, "/c", "exit", "0")
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Give this trivial command time to exit if it was allowed to run. The process
	// must remain alive until attachProcess assigns it to its job and resumes it.
	time.Sleep(500 * time.Millisecond)
	if hasExited(cmd) {
		_ = cmd.Wait()
		t.Fatal("process exited before job assignment")
	}

	control, err := attachProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("attachProcess: %v", err)
	}
	waitErr := cmd.Wait()
	releaseErr := control.release()
	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}
}

func TestHasExitedDetectsProcessBeforeWait(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hasExited(cmd) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hasExited returned false after cmd.exe had exited")
}

func TestWaitReleasesJobAndStopsChildProcesses(t *testing.T) {
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skipf("PowerShell is not available: %v", err)
	}
	command := "$child = Start-Process -FilePath $env:ComSpec -ArgumentList '/c','ping -n 30 127.0.0.1 >NUL' -PassThru; Write-Output $child.Id"
	sim, err := Launch(powershell, "", "-NoProfile", "-Command", command)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := sim.Wait(); err != nil {
		t.Fatalf("Wait: %v\n%s", err, sim.Output())
	}

	fields := strings.Fields(sim.Output())
	if len(fields) == 0 {
		t.Fatalf("PowerShell did not report the child PID: %q", sim.Output())
	}
	pid, err := strconv.ParseUint(fields[len(fields)-1], 10, 32)
	if err != nil {
		t.Fatalf("parsing child PID from %q: %v", sim.Output(), err)
	}
	if err := sim.Stop(); err != nil {
		t.Fatalf("Stop after Wait should be harmless: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return
			}
			t.Fatalf("opening child process %d: %v", pid, err)
		}
		result, waitErr := windows.WaitForSingleObject(process, 0)
		_ = windows.CloseHandle(process)
		if waitErr == nil && result == windows.WAIT_OBJECT_0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child process %d survived closing the Simulator job object", pid)
}
