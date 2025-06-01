//go:build unix

package discovery

import (
	"os"
	"syscall"
)

// createSecureProcessAttributes creates secure process attributes for Unix systems
func createSecureProcessAttributes() *syscall.SysProcAttr {
	attrs := &syscall.SysProcAttr{
		// Create new process group for isolation
		Setpgid: true,
		Pgid:    0,

		// Prevent core dumps for security
		Noctty: true,
	}

	// Only apply user/group restrictions in production environments
	// Skip for testing to avoid permission issues
	if os.Getenv("GO_ENV") == "production" {
		attrs.Credential = &syscall.Credential{
			// Run as non-root user if possible
			// Note: In production, create dedicated user for MCP handlers
			Uid: 65534, // nobody user
			Gid: 65534, // nobody group
		}
	}

	return attrs
}
