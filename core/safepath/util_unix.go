//go:build unix

package safepath

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// randomTmpName returns a hidden tmp filename (`.tmp.<leaf>.<rand>`) suitable
// for the temp+rename atomic write. Hidden so a tree listing doesn't surface
// half-written content; per-call random so two concurrent writers can't share
// a temp path.
func randomTmpName(leaf string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return ".tmp." + leaf + "." + hex.EncodeToString(b[:]), nil
}

// fileInfoFromStat reconstructs an os.FileInfo from a unix.Stat_t result.
// The standard library's stat-to-FileInfo conversion isn't exported; this
// gives SafeLstat a stable return type without dragging in CGo.
type statFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (s *statFileInfo) Name() string       { return s.name }
func (s *statFileInfo) Size() int64        { return s.size }
func (s *statFileInfo) Mode() os.FileMode  { return s.mode }
func (s *statFileInfo) ModTime() time.Time { return s.modTime }
func (s *statFileInfo) IsDir() bool        { return s.isDir }
func (s *statFileInfo) Sys() any           { return nil }

func fileInfoFromStat(st *unix.Stat_t, name string) os.FileInfo {
	mode := os.FileMode(st.Mode & 0o777)
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= os.ModeDir
	case unix.S_IFLNK:
		mode |= os.ModeSymlink
	case unix.S_IFIFO:
		mode |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= os.ModeSocket
	case unix.S_IFBLK, unix.S_IFCHR:
		mode |= os.ModeDevice
	}
	return &statFileInfo{
		name:    name,
		size:    st.Size,
		mode:    mode,
		modTime: timeFromStat(st),
		isDir:   st.Mode&unix.S_IFMT == unix.S_IFDIR,
	}
}
