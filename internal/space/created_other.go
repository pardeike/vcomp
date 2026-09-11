//go:build !darwin

package space

import (
	"os"
	"time"
)

// Portable Go does not expose filesystem birth time. Mark the fallback estimate.
func (r Run) Created() (time.Time, bool) {
	info, err := os.Stat(r.Dir)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
