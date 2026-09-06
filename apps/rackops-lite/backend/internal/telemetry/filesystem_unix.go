//go:build linux || darwin

package telemetry

import "syscall"

func filesystemSpace(path string) (total, available float64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return float64(st.Blocks) * float64(st.Bsize) / 1e9, float64(st.Bavail) * float64(st.Bsize) / 1e9, nil
}
