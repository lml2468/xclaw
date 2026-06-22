//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package safepath

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// isSymlinkErrno: BSDs return one of ELOOP / EFTYPE / EMLINK depending on
// kernel when O_NOFOLLOW refuses a symlink at the leaf.
func isSymlinkErrno(err error) bool {
	return errors.Is(err, unix.ELOOP) ||
		errors.Is(err, unix.EFTYPE) ||
		errors.Is(err, unix.EMLINK)
}

// timeFromStat extracts the modification time from a BSD unix.Stat_t. x/sys/
// unix already normalizes the field to Mtim across darwin/freebsd/netbsd/
// openbsd/dragonfly (vs the raw syscall's Mtimespec); we use it directly.
func timeFromStat(st *unix.Stat_t) time.Time {
	return time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec))
}

// Compile-time assertion that os.FileInfo is satisfied by statFileInfo.
var _ os.FileInfo = (*statFileInfo)(nil)
