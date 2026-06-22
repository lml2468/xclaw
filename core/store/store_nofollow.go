package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lml2468/xclaw/core/safepath"
)

// refuseSymlinkLeaf returns an error if path exists AND is a symlink at the
// leaf. Missing paths are OK (the daemon's first start has no DB yet).
// Used by Open to gate the SQLite open against an agent-planted symlink
// that would redirect schema migrations into an unrelated DB.
//
// Routes through safepath.SafeLstat (dirfd-walk + AT_SYMLINK_NOFOLLOW) per
// CLAUDE.md's policy ("callers MUST NOT do their own Lstat for paths under
// a root"). This narrows but doesn't close the TOCTOU between this check
// and sql.Open's sqlite3_open(); the residual risk is documented and
// bounded by the agent's same-uid scope.
func refuseSymlinkLeaf(path string) error {
	dir, leaf := filepath.Split(path)
	if dir == "" || leaf == "" {
		return fmt.Errorf("refuseSymlinkLeaf: bad path %q", path)
	}
	// Strip the trailing separator so dir is a valid root for SafeLstat.
	dir = filepath.Clean(dir)
	fi, err := safepath.SafeLstat(dir, leaf)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// ErrSymlink (parent or leaf) is the case we're guarding against —
		// surface it; any other error (EACCES, EIO) bubbles up too rather
		// than being misclassified as "no DB yet".
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to open through symlink")
	}
	return nil
}
