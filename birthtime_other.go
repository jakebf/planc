//go:build !darwin && !linux && !windows

package main

import "time"

func fileCreatedTime(_ string, fallback time.Time) time.Time {
	return fallback
}
