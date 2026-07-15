//go:build linux

package server

import "syscall"

// syscallExec replaces the current process image (used after self-update).
func syscallExec(exe string, argv, env []string) error {
	return syscall.Exec(exe, argv, env)
}
