//go:build windows

package installationmanager

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var managerJobOnce sync.Once
var managerJobError error
var managerJob windows.Handle

// The short-lived manager owns this handle until OS process exit. Closing it
// in Run's defer would terminate the manager before its JSON is written. It is
// not inheritable: exit/forced termination closes the last handle and kills all
// descendants, including the PowerShell adapter. No goroutine owns cleanup.
func ensureManagerJob() error {
	managerJobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			managerJobError = err
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			managerJobError = err
			return
		}
		if err = windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
			windows.CloseHandle(job)
			managerJobError = err
			return
		}
		managerJob = job
	})
	return managerJobError
}
