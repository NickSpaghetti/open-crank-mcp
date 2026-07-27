// Package simulator manages PlaydateSimulator as a child process.
package simulator

import (
	"bytes"
	"fmt"
	"os/exec"
	"syscall"
)

// Simulator is a running PlaydateSimulator child process.
type Simulator struct {
	cmd    *exec.Cmd
	output *bytes.Buffer
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

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launching simulator: %w", err)
	}

	return &Simulator{cmd: cmd, output: &output}, nil
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

// Output returns the combined stdout+stderr captured so far. Only safe to
// call after Wait() has returned - reading concurrently with a still-running
// process races with the internal copy goroutines os/exec starts for a
// non-*os.File Stdout/Stderr.
func (s *Simulator) Output() string {
	return s.output.String()
}
