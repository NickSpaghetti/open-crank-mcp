// Package simulator manages PlaydateSimulator as a child process.
package simulator

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
)

// Simulator is a running PlaydateSimulator child process.
type Simulator struct {
	cmd    *exec.Cmd
	output *syncBuffer
}

// syncBuffer guards a bytes.Buffer with a mutex so Output() can be read
// safely while the process is still writing to it, not just after Wait()
// has returned.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// lookPath is a seam for tests, which need both answers without depending on
// what happens to be installed on the machine running them.
var lookPath = exec.LookPath

// lineBufferedCommand wraps a command in `stdbuf -oL` when stdbuf is available, so
// the child's stdout is line-buffered rather than block-buffered.
//
// What this buys, measured as an A/B against a real Simulator through this exact
// code path: the Simulator's own `Loading: <pdx>` startup line arrives immediately
// where before it did not (223 captured bytes without, 288 with, nothing else
// changed). That matters for the failure launch_simulator exists to explain - a
// Simulator that quits during startup, where the one useful message would otherwise
// sit unflushed in a buffer that Stop()'s hard kill discards.
//
// What it does NOT buy, and this was checked rather than assumed: a Lua game's
// print() output still does not appear. The original investigation in
// docs/GOTCHAS.md found the same thing - stdbuf surfaces the Simulator's native
// diagnostics and not its Lua console - and that is why get_game_logs exists and
// why it cannot be retired in favour of get_logs. Curiously, a shell-launched
// Simulator under stdbuf *does* show Lua print(); the difference from this path is
// unexplained and deliberately not claimed either way here.
//
// stdbuf preloads a shim that calls setvbuf before main, so it needs no cooperation
// from the Simulator. It is GNU coreutils: present in the container and on most
// Linux installs, absent on a stock macOS, which native mode targets. A missing
// stdbuf falls back to launching directly rather than failing - better logs are not
// worth a Simulator that will not start.
func lineBufferedCommand(binPath string, args []string) (string, []string) {
	stdbuf, err := lookPath("stdbuf")
	if err != nil {
		return binPath, args
	}
	// Only stdout. stderr is never fully buffered per POSIX, so -eL would be
	// stating a default.
	return stdbuf, append([]string{"-oL", binPath}, args...)
}

// Launch starts binPath (PlaydateSimulator) against pdxPath. Any extraArgs
// are forwarded as additional playdate.argv entries to the running game -
// argv[1] is always the pdx path itself, so extraArgs[0] lands at
// playdate.argv[2], and so on. An empty pdxPath launches the Simulator with
// no game loaded (a real, supported mode - not just a test convenience);
// extraArgs are passed as-is in that case, since there's no game to read
// playdate.argv.
func Launch(binPath, pdxPath string, extraArgs ...string) (*Simulator, error) {
	var args []string
	if pdxPath != "" {
		args = append([]string{pdxPath}, extraArgs...)
	} else {
		args = extraArgs
	}
	name, args := lineBufferedCommand(binPath, args)
	cmd := exec.Command(name, args...)
	setProcAttr(cmd)

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching simulator: %w", err)
	}

	return &Simulator{cmd: cmd, output: output}, nil
}

// Stop kills the Simulator. PlaydateSimulator ignores SIGTERM, so this is
// always a hard kill. Exactly what gets killed is platform-specific and lives
// in proc_unix.go / proc_windows.go: on Unix it is the whole process group,
// because the container launches through a shell that may fork rather than exec
// and leave a grandchild holding the output pipe open.
func (s *Simulator) Stop() error {
	if s.cmd.Process == nil {
		return nil
	}
	if err := killProcess(s.cmd); err != nil {
		return fmt.Errorf("killing simulator: %w", err)
	}
	return nil
}

// Wait blocks until the process exits. Call after Stop() to reap it, or on
// its own if the process is expected to exit by itself.
func (s *Simulator) Wait() error {
	return s.cmd.Wait()
}

// Exited reports whether the process has already finished, without blocking.
// Useful right after Launch: the Simulator can quit during startup - a missing
// PulseAudio makes it refuse to run at all - and a successful Start() says
// nothing about whether it survived.
func (s *Simulator) Exited() bool {
	if s.cmd.Process == nil {
		return true
	}
	return hasExited(s.cmd)
}

// Output returns the combined stdout+stderr captured so far. Safe to call
// at any time, including while the process is still running.
func (s *Simulator) Output() string {
	return s.output.String()
}
