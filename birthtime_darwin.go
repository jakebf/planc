package main

import (
	"syscall"
	"time"
)

func fileCreatedTime(path string, fallback time.Time) time.Time {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err == nil {
		return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec)
	}
	return fallback
}
