//go:build windows

package discovery

import (
	"syscall"
)

// createSecureProcessAttributes creates secure process attributes for Windows systems
func createSecureProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// Create new process group for isolation
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		
		// Hide console window for security
		HideWindow: true,
	}
}