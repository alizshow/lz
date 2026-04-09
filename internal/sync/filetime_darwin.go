package sync

import (
	"syscall"
	"time"
)

// fileTimes returns the creation (birth) and modification times for a file.
func fileTimes(path string) (created, modified time.Time, err error) {
	var st syscall.Stat_t
	if err = syscall.Stat(path, &st); err != nil {
		return
	}
	created = time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	modified = time.Unix(st.Mtimespec.Sec, st.Mtimespec.Nsec)
	return
}
