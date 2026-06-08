package task

import (
	"strings"
	"time"
)

// Status lifecycle.
type Status int

const (
	InProgress Status = iota
	Todo
	Backlog
	Done
	Canceled // opt-in graveyard; hidden unless explicitly requested
)

func (s Status) String() string {
	switch s {
	case InProgress:
		return "In Progress"
	case Todo:
		return "Todo"
	case Backlog:
		return "Backlog"
	case Done:
		return "Done"
	case Canceled:
		return "Canceled"
	}
	return ""
}

// Priority levels (lower value = higher priority, for natural sort).
type Priority int

const (
	PriorityHigh   Priority = 0
	PriorityNormal Priority = 1
	PriorityLow    Priority = 2
)

// Effort levels for task sizing. M is the default (like PriorityNormal).
type Effort int

const (
	EffortS  Effort = iota + 1
	EffortM         // default
	EffortL
	EffortXL
)

func (e Effort) String() string {
	switch e {
	case EffortS:
		return "S"
	case EffortM:
		return "M"
	case EffortL:
		return "L"
	case EffortXL:
		return "XL"
	}
	return ""
}

func ParseEffort(s string) Effort {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "S":
		return EffortS
	case "L":
		return EffortL
	case "XL":
		return EffortXL
	}
	return EffortM
}

// Task is a single task file discovered from a _tasks/ directory.
type Task struct {
	ID       string // stable identity from frontmatter; assigned lazily on first sync
	Title    string
	Filename string
	Project  string
	Scope    string // sub-project path relative to .lz.yml root (e.g. "kube", "launchpad")
	Status   Status
	Priority Priority
	Effort   Effort
	Summary  string
	Path     string
	ModTime  time.Time
}
