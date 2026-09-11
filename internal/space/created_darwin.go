package space

import (
	"os"
	"syscall"
	"time"
)

// Created reports the filesystem birth time, not the mutable directory mtime.
func (r Run) Created() (time.Time, bool) {
	info, err := os.Stat(r.Dir)
	if err != nil {
		return time.Time{}, false
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec), false
	}
	return info.ModTime(), true
}
