package sync

import (
	"syscall"
	"time"
)

// fileTimes returns the creation and modification times for a file. Linux's stat(2)
// carries no birth time, so created falls back to ctime (last inode change) — later
// than true creation whenever the file's metadata has changed since.
func fileTimes(path string) (created, modified time.Time, err error) {
	var st syscall.Stat_t
	if err = syscall.Stat(path, &st); err != nil {
		return
	}
	created = time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
	modified = time.Unix(st.Mtim.Sec, st.Mtim.Nsec)
	return
}
