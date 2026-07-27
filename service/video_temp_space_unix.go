//go:build !windows

package service

import (
	"os"

	"golang.org/x/sys/unix"
)

func videoAvailableBytesForPath(path string) (uint64, error) {
	if path == "" {
		path = os.TempDir()
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	available := uint64(stat.Bavail) * uint64(stat.Bsize)
	if available == 0 {
		return 0, ErrVideoTemporaryStorageUnavailable
	}
	return available, nil
}
