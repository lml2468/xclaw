//go:build windows

package safepath

import (
	"errors"
	"os"
)

// ErrSymlinkLeaf parity on Windows — there's no O_NOFOLLOW analogue, so the
// fallback opens normally and relies on the caller's separate Lstat guard.
// Windows symlinks in an agent sandbox are rare; documented as residual.
var ErrSymlinkLeaf = errors.New("safepath: refusing to follow symlink at open")

func OpenNoFollow(path string) (*os.File, error) { return os.Open(path) }

func WriteNoFollow(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}
