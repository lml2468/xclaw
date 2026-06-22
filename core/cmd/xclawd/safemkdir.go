package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lml2468/xclaw/core/safepath"
)

// safeMkdirAll creates `absPath` and any missing parents, walking via
// safepath's dirfd-based MkdirAll when the path lies under the operator's
// $HOME (so any symlinked intermediate component is refused with
// ErrSymlink — round 20 Sec H4). For an operator-supplied absolute path
// OUTSIDE $HOME we fall back to plain os.MkdirAll: the operator is the
// trust boundary at that point, and the safepath dirfd walk would refuse
// e.g. macOS's /tmp → /private/tmp symlink and break the default.
func safeMkdirAll(absPath string, perm os.FileMode) error {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(absPath, home+string(os.PathSeparator)) {
		rel := filepath.ToSlash(strings.TrimPrefix(absPath, home+string(os.PathSeparator)))
		return safepath.SafeMkdirAll(home, rel, perm)
	}
	return os.MkdirAll(absPath, perm)
}
