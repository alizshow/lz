package main

import (
	"context"
	"fmt"
	"os"
	"aliz/lz/cmd"

	cli "github.com/urfave/cli/v3"
)

func main() {
	root := &cli.Command{
		Name:                  "lz",
		Usage:                 "personal CLI toolkit",
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
				Name:      "setup",
				Usage:     "scaffold a _tasks/ directory and its lifecycle subdirs",
				ArgsUsage: "[path]",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskSetup(c.Args().First())
				},
			},
			{
				Name:  "list",
				Usage: "list tasks (active by default, flags add categories)",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "backlog", Aliases: []string{"b"}, Usage: "include backlog tasks"},
					&cli.BoolFlag{Name: "done", Aliases: []string{"d"}, Usage: "include done tasks"},
					&cli.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "include all categories"},
					&cli.BoolFlag{Name: "exclude-active", Aliases: []string{"x"}, Usage: "exclude active (current + todo)"},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunTaskList(
						c.Bool("backlog"),
						c.Bool("done"),
						c.Bool("all"),
						c.Bool("exclude-active"),
					)
				},
			},
{
				Name:  "sync",
				Usage: "sync tasks to Notion",
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
				Name:  "status",
				Usage: "repo status list",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitStatus()
				},
			},
			{
				Name:  "commits",
				Usage: "recent commits per repo",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitCommits()
				},
			},
			{
				Name:  "stash",
				Usage: "stash entries per repo",
				Action: func(ctx context.Context, c *cli.Command) error {
					return cmd.RunGitStash()
				},
			},
		},
	}
}
