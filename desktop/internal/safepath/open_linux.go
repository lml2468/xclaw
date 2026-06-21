//go:build linux

package safepath

import (
	"errors"
	"syscall"
)

func isSymlinkErrno(err error) bool { return errors.Is(err, syscall.ELOOP) }
