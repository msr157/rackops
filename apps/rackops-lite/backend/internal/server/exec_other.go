//go:build !linux && !darwin

package server

import "fmt"

// syscallExec is unavailable off Linux/macOS; self-update matches that gate.
func syscallExec(exe string, argv, env []string) error {
	return fmt.Errorf("in-place restart is not supported on this platform")
}
