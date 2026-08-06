package main

import (
	"aliz/lz/cmd"
	"context"
	"fmt"
	"os"
	"strings"

	cli "github.com/urfave/cli/v3"
)

func main() {
	root := &cli.Command{
		Name:                   "lz",
		Usage:                  "personal CLI toolkit",
		UseShortOptionHandling: true,
		Commands: []*cli.Command{
			taskCmd(),
			gitCmd(),
		},
	}

	if err := root.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func taskCmd() *cli.Command {
	return &cli.Command{
		Name:  "task",
		Usage: "task browser TUI",
		Action: func(ctx context.Context, c *cli.Command) error {
			return cmd.RunTaskTUI()
		},
		Commands: []*cli.Command{
			{
				Name:      "init",
				Aliases:   []string{"setup"},
				Usage:     "scaffold a _tasks/ directory and its lifecycle subdirs",
				ArgsUsage: "[path]",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskSetup(c.Args().First())
				},
			},
			{
				Name:      "new",
				Aliases:   []string{"n", "add"},
				Usage:     "create a new task file and print its path",
				ArgsUsage: "<title>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "stage", Aliases: []string{"s"}, Usage: "lifecycle stage: backlog, todo, current", Value: "backlog"},
					&cli.StringFlag{Name: "priority", Aliases: []string{"p"}, Usage: "high, normal or low"},
					&cli.StringFlag{Name: "effort", Aliases: []string{"e"}, Usage: "S, M, L or XL"},
					&cli.StringFlag{Name: "summary", Aliases: []string{"m"}, Usage: "one-line summary"},
					&cli.StringFlag{Name: "dir", Aliases: []string{"C"}, Usage: "project dir whose _tasks/ to use (default: nearest above cwd)"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskNew(
						strings.Join(c.Args().Slice(), " "),
						c.String("stage"),
						c.String("priority"),
						c.String("effort"),
						c.String("summary"),
						c.String("dir"),
					)
				},
			},
			{
				Name:    "list",
				Aliases: []string{"l", "ls"},
				Usage:   "list tasks (active by default, flags add categories)",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "backlog", Aliases: []string{"b"}, Usage: "include backlog tasks"},
					&cli.BoolFlag{Name: "done", Aliases: []string{"d"}, Usage: "include done tasks"},
					&cli.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "include all categories"},
					&cli.BoolFlag{Name: "exclude-active", Aliases: []string{"x"}, Usage: "exclude active (current + todo)"},
					&cli.BoolFlag{Name: "canceled", Aliases: []string{"c"}, Usage: "include canceled tasks"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskList(
						c.Bool("backlog"),
						c.Bool("done"),
						c.Bool("all"),
						c.Bool("exclude-active"),
						c.Bool("canceled"),
					)
				},
			},
			{
				Name:    "sync",
				Aliases: []string{"s"},
				Usage:   "sync tasks to Notion",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "dry-run", Aliases: []string{"n"}, Usage: "preview what would be synced"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskSync(c.Bool("dry-run"))
				},
			},
		},
	}
}

func gitCmd() *cli.Command {
	return &cli.Command{
		Name:  "git",
		Usage: "multi-repo git status TUI",
		Action: func(ctx context.Context, c *cli.Command) error {
			return cmd.RunGitTUI()
		},
		Commands: []*cli.Command{
			{
				Name:    "status",
				Aliases: []string{"s", "st"},
				Usage:   "repo status list",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitStatus()
				},
			},
			{
				Name:    "commits",
				Aliases: []string{"c", "log"},
				Usage:   "recent commits per repo",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitCommits()
				},
			},
			{
				Name:    "stash",
				Aliases: []string{"z"},
				Usage:   "stash entries per repo",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitStash()
				},
			},
		},
	}
}
