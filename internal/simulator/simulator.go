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
	cmd     *exec.Cmd
	output  *syncBuffer
	control *processControl
}

// processControl owns platform-specific cleanup for the Simulator process and
// any children it starts.
type processControl struct {
	mu          sync.Mutex
	stopFunc    func() error
	releaseFunc func() error
	stopped     bool
	released    bool
}

func (p *processControl) stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released || p.stopped || p.stopFunc == nil {
		return nil
	}
	if err := p.stopFunc(); err != nil {
		return err
	}
	p.stopped = true
	return nil
}

func (p *processControl) release() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return nil
	}
	p.released = true
	p.stopFunc = nil
	if p.releaseFunc == nil {
		return nil
	}
	err := p.releaseFunc()
	p.releaseFunc = nil
	return err
}

// syncBuffer lets Output() be read while the process is still writing.
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

// lookPath is a seam for tests.
var lookPath = exec.LookPath

// lineBufferedCommand wraps a command in `stdbuf -oL` when available, so startup
// errors are flushed before a hard kill discards them. It does not surface Lua
// print() (see docs/GOTCHAS.md). Without stdbuf (e.g. macOS) it launches directly.
func lineBufferedCommand(binPath string, args []string) (string, []string) {
	stdbuf, err := lookPath("stdbuf")
	if err != nil {
		return binPath, args
	}
	// stderr is never fully buffered per POSIX.
	return stdbuf, append([]string{"-oL", binPath}, args...)
}

// Launch starts binPath (PlaydateSimulator) against pdxPath. extraArgs follow
// pdxPath in playdate.argv. An empty pdxPath launches with no game loaded and
// passes extraArgs as-is.
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
	control, err := attachProcess(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("setting up simulator process control: %w", err)
	}

	return &Simulator{cmd: cmd, output: output, control: control}, nil
}

// Stop hard-kills the Simulator and its descendants; PlaydateSimulator ignores SIGTERM.
func (s *Simulator) Stop() error {
	if s.cmd.Process == nil {
		return nil
	}
	var err error
	if s.control != nil {
		err = s.control.stop()
	} else {
		err = killProcess(s.cmd)
	}
	if err != nil {
		return fmt.Errorf("killing simulator: %w", err)
	}
	return nil
}

// Wait blocks until the process exits. Call after Stop() to reap it, or on
// its own if the process is expected to exit by itself.
func (s *Simulator) Wait() error {
	err := s.cmd.Wait()
	if s.control != nil {
		if releaseErr := s.control.release(); err == nil {
			err = releaseErr
		}
	}
	return err
}

// Exited reports whether the process has already finished, without blocking.
// The Simulator can quit during startup, e.g. without PulseAudio.
func (s *Simulator) Exited() bool {
	if s.cmd.Process == nil {
		return true
	}
	return hasExited(s.cmd)
}

// Output returns the combined stdout+stderr captured so far. Safe while running.
func (s *Simulator) Output() string {
	return s.output.String()
}
