//go:build unix

package simulator

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the child in its own process group so killProcess can take
// the whole group down at once.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func attachProcess(cmd *exec.Cmd) (*processControl, error) {
	return &processControl{stopFunc: func() error { return killProcess(cmd) }}, nil
}

// killProcess SIGKILLs the child's entire process group, so no descendant keeps
// the output pipe open and blocks Wait(). PlaydateSimulator ignores SIGTERM.
func killProcess(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// hasExited reports whether the child is gone, without blocking. Signal 0 also
// errors for a released-but-unreaped child, which counts as gone.
func hasExited(cmd *exec.Cmd) bool {
	return cmd.Process.Signal(syscall.Signal(0)) != nil
}
