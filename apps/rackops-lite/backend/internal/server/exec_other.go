//go:build !linux

package server

import "fmt"

// syscallExec is unavailable off Linux; self-update is Linux-only anyway.
func syscallExec(exe string, argv, env []string) error {
	return fmt.Errorf("in-place restart is not supported on this platform")
}
