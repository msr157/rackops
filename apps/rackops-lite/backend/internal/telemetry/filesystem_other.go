//go:build !linux && !darwin

package telemetry

import "errors"

func filesystemSpace(string) (float64, float64, error) {
	return 0, 0, errors.New("filesystem telemetry is unavailable on this platform; use Linux, macOS or WSL2")
}
