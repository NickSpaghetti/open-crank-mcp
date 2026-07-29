//go:build windows

package simulator

import "os/exec"

// Process control on Windows. Compiled and vetted by `make go-build-cross`, but
// never run: Windows-native is out of scope, and WSL2 covers Windows users
// through the container. See docs/ROADMAP.md. The point of this file is that the
// package builds for windows/amd64 at all, so promoting Windows later is
// additive rather than a rewrite.

// setProcAttr is a no-op. Setpgid has no Windows equivalent on
// syscall.SysProcAttr, and nothing here sends Ctrl+Break, which is the only
// thing CREATE_NEW_PROCESS_GROUP would buy.
func setProcAttr(cmd *exec.Cmd) {}

// killProcess kills the single child process.
//
// Not the process tree. The Unix version kills a group to catch a grandchild
// left behind by a forking shell, which is a container launch shape; a native
// run execs the Simulator directly. If PlaydateSimulator.exe turns out to spawn
// helpers of its own, this needs a job object via golang.org/x/sys/windows
// (already an indirect dependency) rather than a bare Kill.
func killProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

// hasExited reports whether the child is gone, without blocking.
//
// Deliberately not the Unix signal-0 probe. os.Process.Signal on Windows
// returns an error for every signal except Kill, so a signal-0 check would
// always report "gone" - and because it compiles, that wrong answer would
// survive the cross-compile gate silently. ProcessState is set only once Wait
// has reaped the child, which is exactly "has finished".
func hasExited(cmd *exec.Cmd) bool {
	return cmd.ProcessState != nil
}
