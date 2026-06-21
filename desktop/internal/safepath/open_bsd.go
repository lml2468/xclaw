//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package safepath

import (
	"errors"
	"syscall"
)

// macOS, FreeBSD, NetBSD, OpenBSD, Dragonfly all return one of ELOOP / EFTYPE
// / EMLINK from O_NOFOLLOW on a symlink, depending on kernel version.
func isSymlinkErrno(err error) bool {
	return errors.Is(err, syscall.ELOOP) ||
		errors.Is(err, syscall.EFTYPE) ||
		errors.Is(err, syscall.EMLINK)
}
