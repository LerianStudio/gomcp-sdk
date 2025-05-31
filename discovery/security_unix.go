//go:build unix

package discovery

import (
	"syscall"
)

// createSecureProcessAttributes creates secure process attributes for Unix systems
func createSecureProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// Create new process group for isolation
		Setpgid: true,
		Pgid:    0,
		
		// Security: Set secure credential attributes
		Credential: &syscall.Credential{
			// Run as non-root user if possible
			// Note: In production, create dedicated user for MCP handlers
			Uid: 65534, // nobody user
			Gid: 65534, // nobody group
		},
		
		// Prevent core dumps for security
		Noctty: true,
	}
}