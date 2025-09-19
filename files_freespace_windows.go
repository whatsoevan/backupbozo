//go:build windows
// +build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func getFreeSpace(path string) (uint64, error) {
	lpFreeBytesAvailable := uint64(0)
	lpTotalNumberOfBytes := uint64(0)
	lpTotalNumberOfFreeBytes := uint64(0)
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	err = windows.GetDiskFreeSpaceEx(
		p,
		&lpFreeBytesAvailable,
		&lpTotalNumberOfBytes,
		&lpTotalNumberOfFreeBytes,
	)
	if err != nil {
		return 0, err
	}
	return lpFreeBytesAvailable, nil
}