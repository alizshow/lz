package git

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RepoStatus holds parsed git state for a single repo.
type RepoStatus struct {
	Branch      string
	Tag         string
	Ahead       int
	Behind      int
	Stash       int
	HasUpstream bool
	Age         time.Time // last commit time
	Files       []FileStatus
	IsClean     bool
}

// FileStatus is a single porcelain status entry.
type FileStatus struct {
	XY   string // two-char status code
	File string // file path (may contain " -> " for renames)
}

// GetStatus runs git commands and returns parsed status for a repo.
func GetStatus(dir string) RepoStatus {
	var s RepoStatus

	// branch
	s.Branch = gitLine(dir, "branch", "--show-current")
	if s.Branch == "" {
		s.Branch = "HEAD"
	}

	// latest tag
	s.Tag = gitLine(dir, "describe", "--tags", "--abbrev=0")

	// ahead/behind
	upstream := gitLine(dir, "rev-parse", "--abbrev-ref", "@{upstream}")
	s.HasUpstream = upstream != ""
	if s.HasUpstream {
		if v := gitLine(dir, "rev-list", "--count", "@{upstream}..HEAD"); v != "" {
			s.Ahead, _ = strconv.Atoi(v)
		}
		if v := gitLine(dir, "rev-list", "--count", "HEAD..@{upstream}"); v != "" {
			s.Behind, _ = strconv.Atoi(v)
		}
	}

	// stash
	stashOut := gitOutput(dir, "stash", "list")
	if stashOut != "" {
		s.Stash = len(strings.Split(strings.TrimSpace(stashOut), "\n"))
	}

	// last commit time
	ts := gitLine(dir, "log", "-1", "--format=%ct")
	if ts != "" {
		if epoch, err := strconv.ParseInt(ts, 10, 64); err == nil {
			s.Age = time.Unix(epoch, 0)
		}
	}

	// porcelain status
	porcelain := gitOutput(dir, "status", "--porcelain")
	if porcelain == "" {
		s.IsClean = true
	} else {
		for _, line := range strings.Split(strings.TrimRight(porcelain, "\n"), "\n") {
			if len(line) < 3 {
				continue
			}
			s.Files = append(s.Files, FileStatus{
				XY:   line[:2],
				File: line[3:],
			})
		}
	}

	return s
}

// Diff returns the diff output for a single file in a repo.
// It picks the right git command based on the porcelain status code.
func Diff(dir, file, xy string) string {
	// Untracked: show full contents as a diff
	if xy == "??" {
		return gitOutput(dir, "diff", "--no-index", "--", "/dev/null", file)
	}
	// Staged changes (index column has a letter)
	if xy[0] != ' ' {
		return gitOutput(dir, "diff", "--cached", "--", file)
	}
	// Unstaged working-tree changes
	return gitOutput(dir, "diff", "--", file)
}

func gitLine(dir string, args ...string) string {
	return strings.TrimSpace(gitOutput(dir, args...))
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
