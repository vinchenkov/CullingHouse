//go:build linux

package substrate

import (
	"io"
	"os"
)

// platformMountinfo is the real mount table: on Linux, /proc/self/mountinfo
// exists and mc opens the spine for real, so the decision is made from the
// kernel's own view of what is mounted where.
func platformMountinfo() (io.ReadCloser, error) {
	return os.Open("/proc/self/mountinfo")
}
