package main

import (
	"fmt"
	"os"

	"aliz/lz/cmd"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	var err error
	switch os.Args[1] {
	case "t", "tsk":
		err = cmd.RunTsk()
	case "g", "git":
		err = cmd.RunGit()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("lz — personal CLI toolkit")
	fmt.Println()
	fmt.Println("  lz t, lz tsk    task browser TUI [-l/--list] [-a/--all]")
	fmt.Println("  lz g, lz git    multi-repo git status")
}
