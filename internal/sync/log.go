package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const maxLogFiles = 50

// SyncLogger writes timestamped lines to ~/.lz/logs/sync-{timestamp}.log.
type SyncLogger struct {
	file *os.File
}

func NewSyncLogger() (*SyncLogger, error) {
	dir := filepath.Join(StateDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	name := fmt.Sprintf("sync-%s.log", time.Now().Format("20060102-150405"))
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}

	// Cleanup old log files.
	entries, _ := filepath.Glob(filepath.Join(dir, "sync-*.log"))
	if len(entries) > maxLogFiles {
		slices.Sort(entries)
		for _, old := range entries[:len(entries)-maxLogFiles] {
			_ = os.Remove(old)
		}
	}

	return &SyncLogger{file: f}, nil
}

func (l *SyncLogger) Log(format string, args ...any) {
	if l == nil || l.file == nil {
		return
	}
	ts := time.Now().Format(time.RFC3339)
	fmt.Fprintf(l.file, "%s %s\n", ts, fmt.Sprintf(format, args...))
}

func (l *SyncLogger) Close() {
	if l != nil && l.file != nil {
		l.file.Close()
	}
}
