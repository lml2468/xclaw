//go:build linux

package safepath

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// isSymlinkErrno: Linux's O_NOFOLLOW returns ELOOP unconditionally.
func isSymlinkErrno(err error) bool { return errors.Is(err, unix.ELOOP) }

// timeFromStat extracts the modification time from a Linux unix.Stat_t.
// (BSDs and Linux name the field differently — Mtim vs Mtimespec.)
func timeFromStat(st *unix.Stat_t) time.Time {
	return time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec))
}

// Compile-time assertion that os.FileInfo is satisfied by statFileInfo.
var _ os.FileInfo = (*statFileInfo)(nil)
