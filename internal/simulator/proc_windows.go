//go:build windows

package simulator

// Process control on Windows. The Simulator can start a child process that
// outlives the initial executable, so a Windows job object keeps the full
// process tree under the server's control.

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start suspended so the Simulator cannot spawn any children before it has been
// assigned to the job object in attachProcess.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
}

func attachProcess(cmd *exec.Cmd) (*processControl, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	err = windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if err := resumePrimaryThread(uint32(cmd.Process.Pid)); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &processControl{
		stopFunc:    func() error { return windows.TerminateJobObject(job, 1) },
		releaseFunc: func() error { return windows.CloseHandle(job) },
	}, nil
}

// resumePrimaryThread finds the sole thread created with CREATE_SUSPENDED and
// lets it run only after the process is attached to its job object.
func resumePrimaryThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshotting Windows threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("enumerating Windows threads: %w", err)
	}
	for {
		if entry.OwnerProcessID == pid {
			thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if err == nil {
				_, resumeErr := windows.ResumeThread(thread)
				_ = windows.CloseHandle(thread)
				if resumeErr != nil {
					return fmt.Errorf("resuming Simulator thread: %w", resumeErr)
				}
				return nil
			}
		}
		if err := windows.Thread32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return fmt.Errorf("could not find the suspended Simulator thread for process %d", pid)
}

// killProcess kills the single child process.
//
// The job object in attachProcess takes care of descendants as well.
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
	if cmd.ProcessState != nil {
		return true
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(process)
	result, err := windows.WaitForSingleObject(process, 0)
	return err != nil || result == windows.WAIT_OBJECT_0
}
