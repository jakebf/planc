package main

import (
	"os"
	"syscall"
	"time"
)

func fileCreatedTime(path string, fallback time.Time) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return fallback
	}
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return fallback
}
