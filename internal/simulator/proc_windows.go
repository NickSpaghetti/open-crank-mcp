//go:build windows

package simulator

// The Simulator can start a child that outlives it, so a job object holds the whole tree.

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start suspended so the Simulator cannot spawn children before attachProcess
// assigns it to the job object.
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

// resumePrimaryThread resumes the sole thread created with CREATE_SUSPENDED.
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

// killProcess kills the child alone; descendants are the job object's job.
func killProcess(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

// hasExited reports whether the child is gone, without blocking. Not a signal-0
// probe: os.Process.Signal on Windows errors for every signal but Kill.
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
