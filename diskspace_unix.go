//go:build !windows

package main

import "syscall"

// getFreeSpace returns available disk space for the given path (Unix implementation)
func getFreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}