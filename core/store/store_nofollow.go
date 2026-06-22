package store

import (
	"errors"
	"fmt"
	"os"
)

// refuseSymlinkLeaf returns an error if path exists AND is a symlink at the
// leaf. Missing paths are OK (the daemon's first start has no DB yet).
// Used by Open to gate the SQLite open against an agent-planted symlink
// that would redirect schema migrations into an unrelated DB.
func refuseSymlinkLeaf(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to open through symlink")
	}
	return nil
}
