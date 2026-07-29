//go:build unix

package simulator

import (
	"os/exec"
	"syscall"
)

// Process control on Unix. The Windows half lives in proc_windows.go, and the
// two exist because all three of these operations are POSIX-only in different
// ways: two don't compile on Windows at all, and the third compiles and
// silently answers wrong. See each function.

// setProcAttr puts the child in its own process group so killProcess can take
// the whole group down at once.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcess SIGKILLs the child's entire process group.
//
// The group, not just the process: a shell invoked as `sh -c "... simulator"`
// may fork rather than exec its final command, leaving an orphaned grandchild
// holding stdout/stderr's pipe open, which blocks Wait() until it exits on its
// own. That launch shape is how the container runs things. SIGKILL rather than
// SIGTERM because PlaydateSimulator ignores SIGTERM.
func killProcess(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// hasExited reports whether the child is gone, without blocking.
//
// Signal 0 tests for a process's existence without touching it. A
// released-but-unreaped child answers with an error here too, which is what we
// want: either way it is gone.
func hasExited(cmd *exec.Cmd) bool {
	return cmd.Process.Signal(syscall.Signal(0)) != nil
}
