package main

import (
	"time"

	"golang.org/x/sys/unix"
)

func fileCreatedTime(path string, fallback time.Time) time.Time {
	var stat unix.Statx_t
	err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_BTIME, &stat)
	if err == nil && stat.Mask&unix.STATX_BTIME != 0 {
		return time.Unix(int64(stat.Btime.Sec), int64(stat.Btime.Nsec))
	}
	return fallback
}
