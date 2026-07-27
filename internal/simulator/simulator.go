// Package simulator manages PlaydateSimulator as a child process.
package simulator

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
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
	cmd := exec.Command(binPath, args...)

	output := &syncBuffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching simulator: %w", err)
	}

	return &Simulator{cmd: cmd, output: output}, nil
}

// Stop sends SIGKILL. PlaydateSimulator doesn't exit on SIGTERM.
func (s *Simulator) Stop() error {
	if s.cmd.Process == nil {
		return nil
	}
	if err := s.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		return fmt.Errorf("killing simulator: %w", err)
	}
	return nil
}

// Wait blocks until the process exits. Call after Stop() to reap it, or on
// its own if the process is expected to exit by itself.
func (s *Simulator) Wait() error {
	return s.cmd.Wait()
}

// Output returns the combined stdout+stderr captured so far. Safe to call
// at any time, including while the process is still running.
func (s *Simulator) Output() string {
	return s.output.String()
}
